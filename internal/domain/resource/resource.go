// Package resource is the 资源中心 bounded context: registering Tools,
// Skills, MCP servers and knowledge bases, and the rules that protect the
// credentials inside their configs.
//
// spec-05's rule about credentials is enforced here rather than at each
// handler: a credential is *omitted* from any response, not masked and not
// returned as ciphertext, and which keys count as credentials is one
// definition instead of a check per endpoint.
package resource

import (
	"strings"
	"time"
)

// Kind names the four registerable resource types, matching
// components.schemas.ResourceType in api/openapi.yaml. Each lives in its own
// table (spec-05 "分表设计"); that split is a storage detail the Repository
// port hides.
type Kind string

const (
	KindTool          Kind = "tool"
	KindSkill         Kind = "skill"
	KindMCP           Kind = "mcp"
	KindKnowledgeBase Kind = "knowledge_base"
	KindMemory        Kind = "memory"
)

// AllKinds lists every kind, in the order the unfiltered list endpoint
// queries them.
var AllKinds = []Kind{KindTool, KindSkill, KindMCP, KindKnowledgeBase, KindMemory}

func ParseKind(s string) (Kind, bool) {
	switch Kind(s) {
	case KindTool, KindSkill, KindMCP, KindKnowledgeBase, KindMemory:
		return Kind(s), true
	default:
		return "", false
	}
}

// Status is a resource's enabled flag. A disabled resource still exists but
// can no longer be referenced by a new Agent version (30002).
type Status int16

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

// Health is an MCP server's last connectivity result. Empty for the three
// kinds that have no such concept.
type Health string

const (
	HealthUnknown   Health = ""
	HealthHealthy   Health = "healthy"
	HealthUnhealthy Health = "unhealthy"
)

// Resource is one registered resource.
type Resource struct {
	ID          int64
	OwnerID     int64
	Kind        Kind
	Ref         string
	Version     string
	DisplayName string
	Config      Config
	Status      Status
	Health      Health
	CreatedAt   time.Time
}

// Config is a resource's configuration document. Some of its values are
// credentials; which ones is decided by IsCredentialKey, in one place.
type Config map[string]any

// credentialKeySubstrings marks a config field as a credential if its
// lowercased key contains any of these — covering the shapes configs
// actually use (`api_key`, `apiKey`, `auth_token`, a plain `token` /
// `secret` / `password`, ...). Substring matching is deliberately broad:
// wrongly treating a field as secret costs a little inspectability, while
// missing one leaks it.
var credentialKeySubstrings = []string{
	"api_key", "apikey", "token", "secret", "password", "credential", "private_key",
}

// IsCredentialKey reports whether a config key holds a credential.
func IsCredentialKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range credentialKeySubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// Redact returns a copy with every credential field *removed*. spec-05's
// "凭证字段在任何 GET 响应中都不出现" means omitted — not masked with dots,
// not shown as ciphertext, gone.
//
// A "headers" field (an MCP resource's custom header list) gets the same
// treatment one level down: IsCredentialKey can't see inside a []any of
// {key, value} objects, and a custom header's value is unpredictable by
// name — a user can just as easily call it "x-secret-9527" as
// "Authorization" — so every header value is treated as a credential
// unconditionally, keeping only the header name for display.
func (c Config) Redact() Config {
	out := make(Config, len(c))
	for k, v := range c {
		if k == headersConfigKey {
			out[k] = redactHeaderList(v)
			continue
		}
		if IsCredentialKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

// headersConfigKey is the config field name an MCP resource's custom
// header list lives under — []any of {"key": string, "value": string}.
const headersConfigKey = "headers"

func redactHeaderList(v any) any {
	raw, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		out = append(out, map[string]any{"key": key})
	}
	return out
}

// MergePreservingCredentials 把一份"从 GET 读出来又改过"的 config 合回存
// 量，凭据字段不因为读不到而丢失。
//
// 为什么需要它：GET 返回的 config 是 Redact() 过的——凭据字段整个不出现
// （spec-05 的规矩，不是打码也不是密文，是没有）。而编辑一个资源的自然做
// 法就是"读出来、改一个字段、整份存回去"。两件事凑在一起，结果是用户只改
// 了个显示名，api_key 就被那份读不到密钥的 config 覆盖没了——而且是静默
// 的，下次调用才会失败。
//
// 规则：
//   - 凭据键在 incoming 里缺席 → 保留库里的值（这就是"没改密钥"）
//   - 凭据键有非空新值        → 用新值替换
//   - 凭据键显式给空串        → 当作要清除，照办
//
// 非凭据字段一律以 incoming 为准，包括删除：改配置时把某个普通字段去掉，
// 就该真的去掉。
func (c Config) MergePreservingCredentials(stored Config) Config {
	out := make(Config, len(c))
	for k, v := range c {
		out[k] = v
	}
	for k, storedVal := range stored {
		if k == headersConfigKey {
			continue // 头列表在下面单独按 key 对齐
		}
		if !IsCredentialKey(k) {
			continue
		}
		if _, present := c[k]; !present {
			out[k] = storedVal
		}
	}
	if _, present := c[headersConfigKey]; present {
		out[headersConfigKey] = mergeHeaderList(c[headersConfigKey], stored[headersConfigKey])
	}
	return out
}

// mergeHeaderList 按头名字对齐补回值。Redact 只留下了 key，所以一个"值为
// 空"的头几乎总是"这个头我没动"，而不是"我要把它改成空字符串"——真想删掉
// 一个头，把整项从列表里去掉即可，那是明确的动作。
func mergeHeaderList(incoming, stored any) any {
	rawIn, ok := incoming.([]any)
	if !ok {
		return incoming
	}
	storedByKey := make(map[string]string)
	if rawStored, ok := stored.([]any); ok {
		for _, item := range rawStored {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			key, _ := m["key"].(string)
			val, _ := m["value"].(string)
			if key != "" {
				storedByKey[key] = val
			}
		}
	}

	out := make([]any, 0, len(rawIn))
	for _, item := range rawIn {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		val, _ := m["value"].(string)
		merged := map[string]any{"key": key}
		if val != "" {
			merged["value"] = val
		} else if prev, found := storedByKey[key]; found {
			merged["value"] = prev
		} else {
			merged["value"] = ""
		}
		out = append(out, merged)
	}
	return out
}

// AgentReference is an Agent version that references a resource. A narrow
// read model: the resource context needs only enough to explain why a
// delete would be unsafe.
type AgentReference struct {
	AgentRef string
	Version  string
}

// DeleteCheck reports whether a resource can be safely deleted and, if not,
// who is holding it.
type DeleteCheck struct {
	Deletable    bool
	ReferencedBy []AgentReference
}
