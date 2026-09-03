package modelgateway

import (
	"os"
	"testing"

	"github.com/marcon0203/agentic-kit/internal/channeltemplates"
	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// testChannelSpecs 是测试用的那几个渠道。列一处而不是 TestMain 和
// restoreTestChannels 各抄一份——加一个渠道时漏改一处，表现是某些测试之后
// 注册表里少一个渠道，而且只在特定执行顺序下才复现。
var testChannelSpecs = []struct{ template, key, label, baseURL string }{
	{"deepseek", "deepseek", "DeepSeek", ""},
	{"volcengine", "volcengine", "火山引擎方舟", ""},
	{"qwen", "qwen", "通义千问", ""},
	{"zhipu", "zhipu", "智谱 GLM", ""},
	{"openai-compatible", "custom", "自定义端点", "https://custom.test/v1"},
	{"kimi-for-coding", "kimi", "Kimi For Coding", ""},
}

func instantiateTestChannels() ([]*descriptor.Descriptor, error) {
	descs := make([]*descriptor.Descriptor, 0, len(testChannelSpecs))
	for _, s := range testChannelSpecs {
		d, _, err := channeltemplates.Instantiate(s.template, s.key, s.label, s.baseURL)
		if err != nil {
			return nil, err
		}
		descs = append(descs, d)
	}
	return descs, nil
}

// 平台开箱不带任何渠道——渠道是管理员从协议模板建出来的。测试也走同一条
// 路：这里按模板实例化几个渠道装进注册表，等价于管理员在
// 系统配置 → 模型提供商 里建了它们。顺带把 Instantiate 这条路径也覆盖了。
func TestMain(m *testing.M) {
	descs, err := instantiateTestChannels()
	if err != nil {
		panic("测试渠道实例化失败: " + err.Error())
	}
	SetChannels(descs)
	os.Exit(m.Run())
}

// restoreTestChannels 把注册表恢复成 TestMain 建好的那几个渠道，供修改注
// 册表的测试用完收尾。
func restoreTestChannels(t *testing.T) {
	t.Helper()
	descs, err := instantiateTestChannels()
	if err != nil {
		t.Fatalf("测试渠道实例化失败: %v", err)
	}
	SetChannels(descs)
}
