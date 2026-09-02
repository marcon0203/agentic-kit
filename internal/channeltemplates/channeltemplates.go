// Package channeltemplates 是**协议模板**：一份写好了某家厂商线协议怎么调
// 的渠道描述符骨架。
//
// 它们不是"内置渠道"——平台开箱不带任何可用的模型供应商。管理员在
// 系统配置 → 模型提供商 里挑一个模板、填上自己的 key 和接口地址，才生成
// 一个真正可调用的渠道（描述符落库，见 catalog_providers.descriptor）。
//
// 这么设计的理由：模型供应商是**部署方的配置**，不是平台的产品内容。预置
// 一堆调不通（网络不通、没账号）的渠道，只会让用户在"配了 key 也用不了"
// 上浪费时间；而从零开始写一份描述符对非开发者又太难。模板正好卡在中间。
//
// 实例化出来的描述符是**快照**：以后模板改了，已经建好的渠道不受影响。渠
// 道的行为不应该在管理员毫不知情的情况下随一次升级变掉。
package channeltemplates

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

//go:embed *.json fixtures/*.json
var files embed.FS

// Template 是模板的对外形状：管理员在前端看到的选项。
type Template struct {
	ID           string                       `json:"id"`
	Label        string                       `json:"label"`
	Description  string                       `json:"description"`
	Wire         string                       `json:"wire"`
	BaseURL      string                       `json:"base_url"`
	Capabilities []string                     `json:"capabilities"`
	Credentials  []descriptor.CredentialField `json:"credentials"`
}

// List 返回全部模板，按 id 排序。每份模板在这里都会被完整加载 + 跑一遍
// fixtures——模板本身写错了要在进程启动时炸，而不是等某个管理员照着它建了
// 渠道之后才发现。
func List() ([]Template, error) {
	names, err := templateFiles()
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(names))
	for _, name := range names {
		d, err := load(name)
		if err != nil {
			return nil, err
		}
		out = append(out, Template{
			ID: d.ID, Label: d.Label, Description: d.Description, Wire: d.Wire,
			BaseURL: d.BaseURL, Capabilities: d.Capabilities, Credentials: d.Credentials,
		})
	}
	return out, nil
}

// Instantiate 用模板生成一份渠道描述符：把 id 换成管理员起的渠道 key、
// label 换成显示名、base_url 换成实际接口地址，然后**完整校验 + 跑一遍
// fixtures**。返回的 JSON 就是要落库的那份快照。
//
// baseURL 留空时沿用模板的默认值；模板自己也没有默认值（openai-compatible
// 这种）时报错——一个连地址都没有的渠道存下来只会在第一次调用时失败，不如
// 在创建时就拦住。
func Instantiate(templateID, key, label, baseURL string) (*descriptor.Descriptor, []byte, error) {
	raw, err := files.ReadFile(templateID + ".json")
	if err != nil {
		return nil, nil, fmt.Errorf("没有名为 %q 的协议模板", templateID)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("模板 %s 不是合法 JSON: %w", templateID, err)
	}

	doc["id"] = key
	if label != "" {
		doc["label"] = label
	}
	if baseURL != "" {
		doc["base_url"] = strings.TrimRight(baseURL, "/")
	}
	if s, _ := doc["base_url"].(string); s == "" {
		return nil, nil, fmt.Errorf("模板 %s 没有默认接口地址，必须填写 base_url", templateID)
	}

	rendered, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	d, err := descriptor.Load(rendered)
	if err != nil {
		return nil, nil, err
	}
	if err := verify(d); err != nil {
		return nil, nil, err
	}
	return d, rendered, nil
}

// LoadStored 加载一份已经落库的渠道描述符。它同样走完整校验——库里的 JSON
// 也可能是被手工改过的，或者是老版本规范存下来的。
func LoadStored(raw []byte) (*descriptor.Descriptor, error) {
	return descriptor.Load(raw)
}

func templateFiles() ([]string, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func load(name string) (*descriptor.Descriptor, error) {
	raw, err := files.ReadFile(name)
	if err != nil {
		return nil, err
	}
	d, err := descriptor.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("协议模板 %s: %w", name, err)
	}
	if d.ID != strings.TrimSuffix(name, ".json") {
		return nil, fmt.Errorf("协议模板 %s 的 id 是 %q，必须和文件名一致", name, d.ID)
	}
	if err := verify(d); err != nil {
		return nil, fmt.Errorf("协议模板 %s: %w", name, err)
	}
	return d, nil
}

// verify 跑这个线协议族的 fixtures。fixtures 按 wire 组织而不是按渠道：
// deepseek / 火山方舟 / 通义千问 / 任意 OpenAI 兼容端点说的都是
// openai.chat.v1，共用一套用例——新增一个同族模板立刻就有回归覆盖。
func verify(d *descriptor.Descriptor) error {
	if d.Wire == "" {
		return nil
	}
	raw, err := files.ReadFile(path.Join("fixtures", d.Wire+".json"))
	if err != nil {
		// 没有 fixtures 不是错误：一个还没人写用例的新线协议族应该能用。
		// 它只是没有回归保障，这是模板作者自己的选择。
		return nil
	}
	var fixtures []descriptor.Fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		return fmt.Errorf("解析 fixtures/%s.json: %w", d.Wire, err)
	}
	return d.Verify(fixtures)
}
