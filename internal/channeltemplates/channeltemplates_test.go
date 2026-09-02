package channeltemplates

import (
	"encoding/json"
	"strings"
	"testing"
)

// 模板本身写错了要在进程启动时炸——List 会完整加载每一份并跑一遍 fixtures。
func TestList_AllTemplatesLoadAndPassFixtures(t *testing.T) {
	items, err := List()
	if err != nil {
		t.Fatalf("模板加载失败: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("一个模板都没有")
	}
	for _, tmpl := range items {
		if tmpl.Label == "" || tmpl.Wire == "" {
			t.Errorf("模板 %s 缺少 label 或 wire: %+v", tmpl.ID, tmpl)
		}
		if len(tmpl.Credentials) == 0 {
			t.Errorf("模板 %s 没有声明任何凭据字段", tmpl.ID)
		}
	}
}

// 这条是整个"新建模型提供商"业务的核心：管理员挑一个模板、起个 key、填个
// 地址，产出一份可直接落库的渠道描述符。
func TestInstantiate_RendersACallableChannel(t *testing.T) {
	d, raw, err := Instantiate("deepseek", "my-deepseek", "我的 DeepSeek", "https://proxy.example.com/v1")
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}
	if d.ID != "my-deepseek" || d.Label != "我的 DeepSeek" {
		t.Errorf("身份没被替换: id=%s label=%s", d.ID, d.Label)
	}
	if d.BaseURL != "https://proxy.example.com/v1" {
		t.Errorf("接口地址没被替换: %s", d.BaseURL)
	}
	if !d.Has("stream") || d.Stream == nil {
		t.Error("实例化出来的渠道应保留模板的流式能力")
	}
	// 落库的是渲染后的快照——以后模板改了，已经建好的渠道不受影响。
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("快照不是合法 JSON: %v", err)
	}
	if stored["id"] != "my-deepseek" {
		t.Errorf("快照里的 id 不对: %v", stored["id"])
	}
}

// 尾部斜杠会让拼出来的路径变成 //chat/completions，有些网关直接 404。
func TestInstantiate_TrimsTrailingSlashFromBaseURL(t *testing.T) {
	d, _, err := Instantiate("deepseek", "x", "X", "https://proxy.example.com/v1/")
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}
	if strings.HasSuffix(d.BaseURL, "/") {
		t.Errorf("接口地址不该带尾部斜杠: %s", d.BaseURL)
	}
}

// 模板没有默认地址（openai-compatible 这种）又不填，存下来只会在第一次调
// 用时失败——在创建时就拦住。
func TestInstantiate_RequiresBaseURLWhenTemplateHasNoDefault(t *testing.T) {
	_, _, err := Instantiate("openai-compatible", "x", "X", "")
	if err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("期望要求填写接口地址，得到: %v", err)
	}
	if _, _, err := Instantiate("openai-compatible", "x", "X", "https://vllm.internal/v1"); err != nil {
		t.Fatalf("填了地址就该能建出来: %v", err)
	}
}

func TestInstantiate_RejectsUnknownTemplate(t *testing.T) {
	if _, _, err := Instantiate("does-not-exist", "x", "X", "https://a.test"); err == nil {
		t.Fatal("不存在的模板必须报错")
	}
}

// key 会成为描述符的 id，非法的 key 要在这一步就被描述符的加载期校验挡住。
func TestInstantiate_RejectsKeyThatBreaksTheDescriptor(t *testing.T) {
	if _, _, err := Instantiate("deepseek", "", "X", "https://a.test"); err == nil {
		t.Fatal("空 key 必须报错")
	}
}

// LoadStored 走的是同一套完整校验：库里的 JSON 也可能被手工改过，或者是老
// 版本规范存下来的。
func TestLoadStored_ValidatesTheSnapshot(t *testing.T) {
	_, raw, err := Instantiate("qwen", "qwen", "通义千问", "")
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}
	if _, err := LoadStored(raw); err != nil {
		t.Fatalf("刚渲染出来的快照应该能加载: %v", err)
	}
	if _, err := LoadStored([]byte(`{"descriptor_version":1,"id":"x"}`)); err == nil {
		t.Fatal("残缺的快照必须被拒绝")
	}
}
