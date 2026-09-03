// Package plugin is the 插件体系 bounded context (spec-20): third-party
// compiled code the platform runs on a user's behalf, as distinct from the
// resource context's four kinds which are all user-filled configuration.
// "组件是数据，插件是代码" — a plugin has a version history and a signature,
// neither of which any resource kind needs, which is why it gets its own
// two tables instead of folding into the resource fan-out.
package plugin

import (
	"sort"
	"strings"
	"time"
)

// HostAPIVersion is the platform's current plugin host API version. A
// manifest's requires.host_api range (schemas/plugin.schema.json) is
// checked against this at install/run time — not at upload time, so a
// plugin built against a future host API can still be uploaded and simply
// can't be installed yet.
const HostAPIVersion = "1.0"

// Status is a plugin version's or an installation's enabled flag, mirroring
// the resource context's StatusDisabled/StatusEnabled convention.
type Status int16

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

// Visibility controls whether a plugin version appears in market listings.
// Every upload starts Private — the publisher can install and test their
// own upload immediately without waiting on review — and only a deliberate
// SetVisibility(Public) call enters it into the moderation queue.
type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityPublic  Visibility = "public"
)

// ReviewStatus is a public plugin version's moderation state. Private
// versions are always Pending and the field is simply ignored for them.
type ReviewStatus string

const (
	ReviewPending  ReviewStatus = "pending"
	ReviewPassed   ReviewStatus = "passed"
	ReviewRejected ReviewStatus = "rejected"
)

// Resolution controls when an installation's pinned version is re-resolved
// (spec-20 §2.1). Pinned — the only mode P1 implements — resolves once at
// run start and holds it for the whole run. Live is reserved for P7.
type Resolution string

const (
	ResolutionPinned Resolution = "pinned"
	ResolutionLive   Resolution = "live"
)

// Plugin is one published version of a plugin package.
type Plugin struct {
	ID           int64
	PluginID     string // reverse-domain id, e.g. acme.charts
	Version      string // semver
	Manifest     map[string]any
	OSSPrefix    string
	PublisherID  *int64 // nil = platform built-in
	Signature    string
	Visibility   Visibility
	ReviewStatus ReviewStatus
	Status       Status
	CreatedAt    time.Time
}

// Config is an installation's own configuration/credentials document. Which
// fields count as credentials follows the same convention as the resource
// context's Config, kept as an independent copy rather than an import: the
// two bounded contexts don't share a dependency for what is, for each of
// them, an internal detail.
type Config map[string]any

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

// Redact returns a copy with every credential field removed, for any
// response that returns an installation's config.
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

// CredentialKeys 返回这份配置里凭据字段的名字，排序后返回。
//
// 名字得露出来，值不能。没有它，"装完之后查看/更换密钥"这件事在界面上是做
// 不出来的：凭据被 Redact 抹得一干二净，前端既不知道有没有、也不知道叫什
// 么，连一个输入框都渲染不出来。
func (c Config) CredentialKeys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		if IsCredentialKey(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// MergePreservingCredentials 把一份"从接口读出来又改过"的配置合回存量，凭
// 据不因为读不到而丢失。与 resource.Config 上的同名方法是同一套规则，理由
// 也一样：读路径 Redact 过，而更新是整体替换，两者一凑，用户改个别的字段
// 就把密钥静默清空了。
//
//   - 凭据键缺席   → 保留库里的值（"我没改密钥"）
//   - 给了非空新值 → 替换
//   - 显式给空串   → 清除
//
// 非凭据字段一律以提交为准，包括删除。
func (c Config) MergePreservingCredentials(stored Config) Config {
	out := make(Config, len(c))
	for k, v := range c {
		out[k] = v
	}
	for k, storedVal := range stored {
		if !IsCredentialKey(k) {
			continue
		}
		if _, present := c[k]; !present {
			out[k] = storedVal
		}
	}
	return out
}

// InstalledTool is one capabilities.tools[]-referenceable action an
// installed plugin version exposes (its manifest's extensions.tools[]
// entries) — what the Agent editor's capability picker needs to let a
// user add "plugin:{plugin_id}/{tool_name}" to an Agent the same way they
// pick any other resource ref (spec-20 §5.1: "不新增字段").
type InstalledTool struct {
	Ref               string // "plugin:{plugin_id}/{tool_name}"
	PluginID          string
	PluginDisplayName string // manifest.display_name，中文展示名
	ToolName          string
	Description       string
}

// Installation is one account's install of one plugin, pinned to a version.
type Installation struct {
	ID          int64
	OwnerUserID int64
	PluginID    string
	Version     string
	Resolution  Resolution
	Config      Config
	// Granted is the subset of the manifest's requires.permissions the
	// owner actually granted at install time — a plugin can declare wanting
	// more than it is given.
	Granted   []string
	Status    Status
	CreatedAt time.Time
	// CredentialKeys 是这次安装填了哪几个凭据字段的名字，值一概没有。界面
	// 靠它渲染"更换密钥"的位置——被 Redact 抹掉之后，前端否则既不知道有没
	// 有、也不知道叫什么。
	CredentialKeys []string
}

// redactConfig 记下凭据键的名字，再把值抹掉。顺序不能反，两件事也不能分
// 开——所有对外返回安装记录的路径都走这一个方法。
func (i *Installation) redactConfig() {
	i.CredentialKeys = i.Config.CredentialKeys()
	i.Config = i.Config.Redact()
}
