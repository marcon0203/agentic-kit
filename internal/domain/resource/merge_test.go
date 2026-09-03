package resource_test

import (
	"testing"

	"github.com/marcon0203/agentic-kit/internal/domain/resource"
)

// 这一组盯的是编辑资源时最容易静默出事的一处：GET 返回的 config 是
// Redact 过的（凭据字段整个不出现），用户改完存回来，如果直接整体替换，
// 密钥就没了——而且当场不报错，下次真调用才失败。

// 缺席 = 没改。这是编辑页最常见的一次提交：只动了显示名/描述。
func TestMerge_AbsentCredentialKeepsStoredValue(t *testing.T) {
	stored := resource.Config{"endpoint": "https://api.example.com", "api_key": "sk-live-123"}
	// 前端拿到的是这份（api_key 被 Redact 掉了），改了 endpoint 就存回来。
	incoming := resource.Config{"endpoint": "https://api.example.com/v2"}

	got := incoming.MergePreservingCredentials(stored)

	if got["api_key"] != "sk-live-123" {
		t.Errorf("没提到的凭据必须保留，得到 %v", got["api_key"])
	}
	if got["endpoint"] != "https://api.example.com/v2" {
		t.Errorf("普通字段应以新值为准，得到 %v", got["endpoint"])
	}
}

// 给了新值就换。
func TestMerge_NewCredentialValueReplaces(t *testing.T) {
	stored := resource.Config{"api_key": "sk-old"}
	got := resource.Config{"api_key": "sk-new"}.MergePreservingCredentials(stored)
	if got["api_key"] != "sk-new" {
		t.Errorf("给了新密钥就该换掉，得到 %v", got["api_key"])
	}
}

// 显式给空串 = 真要清掉。得留这条路，否则密钥只能加不能删。
func TestMerge_ExplicitEmptyClearsCredential(t *testing.T) {
	stored := resource.Config{"api_key": "sk-old"}
	got := resource.Config{"api_key": ""}.MergePreservingCredentials(stored)
	if v, ok := got["api_key"]; !ok || v != "" {
		t.Errorf("显式清空应当照办，得到 %v (present=%v)", v, ok)
	}
}

// 普通字段被去掉就是真去掉——改配置时删掉一项，不该被存量又补回来。
func TestMerge_NonCredentialRemovalIsHonoured(t *testing.T) {
	stored := resource.Config{"endpoint": "https://x", "timeout_seconds": 30}
	got := resource.Config{"endpoint": "https://x"}.MergePreservingCredentials(stored)
	if _, ok := got["timeout_seconds"]; ok {
		t.Error("被删掉的普通字段不该复活")
	}
}

// MCP 的头列表：Redact 只留下了头的名字，值是空的。所以"值为空"几乎总是
// "这个头我没动"，按名字把值补回来。
func TestMerge_HeaderValuesComeBackByName(t *testing.T) {
	stored := resource.Config{"headers": []any{
		map[string]any{"key": "Authorization", "value": "Bearer secret"},
		map[string]any{"key": "X-Tenant", "value": "acme"},
	}}
	// 前端拿到的是只有 key 的那份，用户新加了一个头。
	incoming := resource.Config{"headers": []any{
		map[string]any{"key": "Authorization"},
		map[string]any{"key": "X-Tenant"},
		map[string]any{"key": "X-New", "value": "fresh"},
	}}

	got := incoming.MergePreservingCredentials(stored)
	list, ok := got["headers"].([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("头列表形状不对: %v", got["headers"])
	}
	byKey := map[string]string{}
	for _, item := range list {
		m := item.(map[string]any)
		k, _ := m["key"].(string)
		v, _ := m["value"].(string)
		byKey[k] = v
	}
	if byKey["Authorization"] != "Bearer secret" {
		t.Errorf("原有头的值必须补回来，得到 %q", byKey["Authorization"])
	}
	if byKey["X-Tenant"] != "acme" {
		t.Errorf("原有头的值必须补回来，得到 %q", byKey["X-Tenant"])
	}
	if byKey["X-New"] != "fresh" {
		t.Errorf("新加的头应保留新值，得到 %q", byKey["X-New"])
	}
}

// 把整项从列表里去掉 = 删掉这个头。这是删头的明确动作。
func TestMerge_RemovedHeaderStaysRemoved(t *testing.T) {
	stored := resource.Config{"headers": []any{
		map[string]any{"key": "Authorization", "value": "Bearer secret"},
		map[string]any{"key": "X-Gone", "value": "bye"},
	}}
	incoming := resource.Config{"headers": []any{map[string]any{"key": "Authorization"}}}

	got := incoming.MergePreservingCredentials(stored)
	list := got["headers"].([]any)
	if len(list) != 1 {
		t.Fatalf("去掉的头不该复活，得到 %v", list)
	}
	if list[0].(map[string]any)["value"] != "Bearer secret" {
		t.Errorf("留下的那个头的值要补回来: %v", list[0])
	}
}

// 一次完整的往返：Redact 之后原样存回去，凭据一个都不能少。这条是前面几
// 条的合起来的验收——编辑页真实的一次"什么都没改就点保存"。
func TestMerge_RedactRoundTripLosesNothing(t *testing.T) {
	stored := resource.Config{
		"endpoint": "https://api.example.com",
		"api_key":  "sk-live-123",
		"headers":  []any{map[string]any{"key": "Authorization", "value": "Bearer t"}},
	}

	roundTripped := stored.Redact().MergePreservingCredentials(stored)

	if roundTripped["api_key"] != "sk-live-123" {
		t.Errorf("往返之后密钥丢了: %v", roundTripped["api_key"])
	}
	hdr := roundTripped["headers"].([]any)[0].(map[string]any)
	if hdr["value"] != "Bearer t" {
		t.Errorf("往返之后头的值丢了: %v", hdr)
	}
	if roundTripped["endpoint"] != "https://api.example.com" {
		t.Errorf("普通字段也该原样在: %v", roundTripped["endpoint"])
	}
}
