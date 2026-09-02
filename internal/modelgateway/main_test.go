package modelgateway

import (
	"os"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/channeltemplates"
	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// 平台开箱不带任何渠道——渠道是管理员从协议模板建出来的。测试也走同一条
// 路：这里按模板实例化几个渠道装进注册表，等价于管理员在
// 系统配置 → 模型提供商 里建了它们。顺带把 Instantiate 这条路径也覆盖了。
func TestMain(m *testing.M) {
	specs := []struct{ template, key, label, baseURL string }{
		{"deepseek", "deepseek", "DeepSeek", ""},
		{"volcengine", "volcengine", "火山引擎方舟", ""},
		{"qwen", "qwen", "通义千问", ""},
		{"openai-compatible", "custom", "自定义端点", "https://custom.test/v1"},
	}
	descs := make([]*descriptor.Descriptor, 0, len(specs))
	for _, s := range specs {
		d, _, err := channeltemplates.Instantiate(s.template, s.key, s.label, s.baseURL)
		if err != nil {
			panic("测试渠道实例化失败: " + err.Error())
		}
		descs = append(descs, d)
	}
	SetChannels(descs)
	os.Exit(m.Run())
}

// restoreTestChannels 把注册表恢复成 TestMain 建好的那几个渠道，供修改注
// 册表的测试用完收尾。
func restoreTestChannels(t *testing.T) {
	t.Helper()
	specs := []struct{ template, key, label, baseURL string }{
		{"deepseek", "deepseek", "DeepSeek", ""},
		{"volcengine", "volcengine", "火山引擎方舟", ""},
		{"qwen", "qwen", "通义千问", ""},
		{"openai-compatible", "custom", "自定义端点", "https://custom.test/v1"},
	}
	descs := make([]*descriptor.Descriptor, 0, len(specs))
	for _, s := range specs {
		d, _, err := channeltemplates.Instantiate(s.template, s.key, s.label, s.baseURL)
		if err != nil {
			t.Fatalf("测试渠道实例化失败: %v", err)
		}
		descs = append(descs, d)
	}
	SetChannels(descs)
}
