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
func (c Config) Redact() Config {
	out := make(Config, len(c))
	for k, v := range c {
		if IsCredentialKey(k) {
			continue
		}
		out[k] = v
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
