package plugin

import (
	"fmt"
	"sort"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// ConfigField 是安装时要用户填的一项。清单里的 requires.config_schema 就
// 是它的数组。
//
// 没有这段声明，安装界面根本不知道该问用户要什么——所以需要凭据的插件要么
// 装不上，要么只能像连接器那样在前端为每个插件写一段硬编码的表单。
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Default     string `json:"default,omitempty"`
}

// ConfigSchema 从 manifest 里读出 requires.config_schema。清单是
// map[string]any（上传上来的 JSON 原样存着），所以这里手工取字段而不是反
// 序列化到结构体——多一层结构体就多一处会和 JSON Schema 漂移的地方。
func ConfigSchema(manifest map[string]any) []ConfigField {
	requires, _ := manifest["requires"].(map[string]any)
	raw, _ := requires["config_schema"].([]any)
	out := make([]ConfigField, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key, _ := m["key"].(string)
		if key == "" {
			continue
		}
		f := ConfigField{Key: key}
		f.Label, _ = m["label"].(string)
		f.Description, _ = m["description"].(string)
		f.Required, _ = m["required"].(bool)
		f.Secret, _ = m["secret"].(bool)
		f.Placeholder, _ = m["placeholder"].(string)
		f.Default, _ = m["default"].(string)
		if f.Label == "" {
			f.Label = key
		}
		out = append(out, f)
	}
	return out
}

// validateConfigSchema 在上传/内置播种时把清单里自相矛盾的声明挡下来。
//
// 只有一条规则，但它是这套东西的地基：**声明为 secret 的字段，键名必须让
// 宿主认得出是凭据**（IsCredentialKey）。宿主加解密和脱敏全都只按键名判
// 断，一个叫 oss_ak 的字段就算标了 secret=true 也会明文落库、并且原样出现
// 在每个响应里——插件作者以为自己声明了保密，实际什么都没发生。与其让这
// 种清单装上去，不如在这里拒绝，并把话说清楚。
func validateConfigSchema(manifest map[string]any) error {
	var offenders []string
	for _, f := range ConfigSchema(manifest) {
		if f.Secret && !IsCredentialKey(f.Key) {
			offenders = append(offenders, f.Key)
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)
	return domain.Invalid(domain.CodePluginManifestInvalid, fmt.Sprintf(
		"config_schema 里 %s 声明了 secret，但键名不含 %s 之一——宿主只按键名决定加不加密，"+
			"这样声明的字段会明文落库并出现在响应里。请改名（如 access_key_secret）。",
		strings.Join(offenders, "、"), strings.Join(credentialKeySubstrings, " / ")))
}

// ValidateInstallConfig 按清单的 config_schema 校验一次安装提交的配置：必
// 填项不能空。多余的键不拦——插件以后加字段时，老的安装记录不该因此装不回去。
func ValidateInstallConfig(manifest map[string]any, config Config) error {
	var errs []domain.FieldError
	for _, f := range ConfigSchema(manifest) {
		if !f.Required {
			continue
		}
		v, present := config[f.Key]
		str, _ := v.(string)
		if !present || strings.TrimSpace(str) == "" {
			errs = append(errs, domain.FieldError{Field: "config." + f.Key, Reason: f.Label + " 不能为空"})
		}
	}
	if len(errs) > 0 {
		return domain.Invalid(domain.CodeValidationFailed, "插件配置填写不完整").WithDetails(errs...)
	}
	return nil
}
