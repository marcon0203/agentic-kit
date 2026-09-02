package descriptor

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Fixture 是一个渠道自带的回归用例。声明式最大的风险是"错了不知道"——
// 手写 client 编译期能挡住的错误，描述符要到运行时才炸，报错还是"路径取
// 不到值"这种没法定位的形式。fixtures 就是对症的那副药：**装的时候跑一
// 遍，过不了不给装。**
type Fixture struct {
	Name    string  `json:"name"`
	Request Request `json:"request"`

	// ExpectHTTP 校验渲染出的请求。Body 是子集匹配——只断言你关心的字
	// 段，加一个可选参数不会把所有 fixture 都弄失败。
	ExpectHTTP *struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	} `json:"expect_http"`

	CompleteResponse json.RawMessage `json:"complete_response"`
	StreamResponse   string          `json:"stream_response"`

	ExpectResult *Result `json:"expect_result"`
}

// Verify 跑一份描述符的全部 fixtures。
//
// ★ 这里是整套设计里性价比最高的一条：**同一份 expect_result 同时校验
// complete_response 和 stream_response。** 它强制"流式和非流式产出同构结
// 果"，把声明式最容易出的那类 bug（流式漏了 usage、tool_call 的 id 没接
// 上、reasoning 只在一条路径上有）在装机时就挡掉，而不是等某天有人发现
// 成本统计里流式请求全是 0。
func (d *Descriptor) Verify(fixtures []Fixture) error {
	var problems []string
	for i, f := range fixtures {
		name := f.Name
		if name == "" {
			name = fmt.Sprintf("fixtures[%d]", i)
		}
		for _, p := range d.verifyOne(f) {
			problems = append(problems, name+": "+p)
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("descriptor %q 的 fixtures 没通过:\n  - %s", d.ID, strings.Join(problems, "\n  - "))
	}
	return nil
}

func (d *Descriptor) verifyOne(f Fixture) []string {
	var problems []string

	if f.ExpectHTTP != nil {
		built, err := d.BuildComplete(f.Request)
		if err != nil {
			problems = append(problems, "渲染请求失败: "+err.Error())
		} else {
			if f.ExpectHTTP.Method != "" && built.Method != f.ExpectHTTP.Method {
				problems = append(problems, fmt.Sprintf("method 期望 %s，得到 %s", f.ExpectHTTP.Method, built.Method))
			}
			if f.ExpectHTTP.Path != "" && built.Path != f.ExpectHTTP.Path {
				problems = append(problems, fmt.Sprintf("path 期望 %s，得到 %s", f.ExpectHTTP.Path, built.Path))
			}
			body, _ := built.Body.(map[string]any)
			problems = append(problems, subsetDiff("body", f.ExpectHTTP.Body, body)...)
		}
	}

	if f.ExpectResult == nil {
		return problems
	}

	if len(f.CompleteResponse) > 0 {
		got, err := d.ParseComplete(f.CompleteResponse)
		if err != nil {
			problems = append(problems, "解析非流式响应失败: "+err.Error())
		} else {
			problems = append(problems, resultDiff("非流式", *f.ExpectResult, got)...)
		}
	}

	if f.StreamResponse != "" {
		if d.Stream == nil {
			problems = append(problems, "给了 stream_response 但描述符没有 stream 段")
		} else {
			got, err := d.RunStream(strings.NewReader(f.StreamResponse), nil)
			if err != nil {
				problems = append(problems, "跑流式失败: "+err.Error())
			} else {
				problems = append(problems, resultDiff("流式", *f.ExpectResult, got)...)
			}
		}
	}
	return problems
}

func resultDiff(label string, want, got Result) []string {
	var out []string
	if want.Content != got.Content {
		out = append(out, fmt.Sprintf("%s content 期望 %q，得到 %q", label, want.Content, got.Content))
	}
	if want.Reasoning != got.Reasoning {
		out = append(out, fmt.Sprintf("%s reasoning 期望 %q，得到 %q", label, want.Reasoning, got.Reasoning))
	}
	if want.InputTokens != got.InputTokens {
		out = append(out, fmt.Sprintf("%s input_tokens 期望 %d，得到 %d", label, want.InputTokens, got.InputTokens))
	}
	if want.OutputTokens != got.OutputTokens {
		out = append(out, fmt.Sprintf("%s output_tokens 期望 %d，得到 %d", label, want.OutputTokens, got.OutputTokens))
	}
	if len(want.ToolCalls) != len(got.ToolCalls) {
		out = append(out, fmt.Sprintf("%s 工具调用数期望 %d，得到 %d", label, len(want.ToolCalls), len(got.ToolCalls)))
		return out
	}
	for i := range want.ToolCalls {
		w, g := want.ToolCalls[i], got.ToolCalls[i]
		if w.ID != g.ID || w.Name != g.Name || !reflect.DeepEqual(normalize(w.Arguments), normalize(g.Arguments)) {
			out = append(out, fmt.Sprintf("%s 工具调用[%d] 期望 %v，得到 %v", label, i, w, g))
		}
	}
	return out
}

// normalize 把参数过一遍 JSON，消掉 int/float64 这类只在 Go 侧存在的类型
// 差异——fixtures 里的期望值是从 JSON 解出来的，实际值是从上游解出来的，
// 两边都该按 JSON 的类型系统比。
func normalize(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

// subsetDiff 做子集匹配：只检查 want 里出现的键。整体相等匹配会让每加一
// 个可选参数就得改所有 fixture，那样 fixtures 会先被人嫌弃，然后被删掉。
func subsetDiff(path string, want, got map[string]any) []string {
	var out []string
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sub := path + "." + k
		gv, ok := got[k]
		if !ok {
			out = append(out, fmt.Sprintf("%s 缺失（期望 %v）", sub, want[k]))
			continue
		}
		wm, wok := want[k].(map[string]any)
		gm, gok := gv.(map[string]any)
		if wok && gok {
			out = append(out, subsetDiff(sub, wm, gm)...)
			continue
		}
		if !reflect.DeepEqual(normalizeAny(want[k]), normalizeAny(gv)) {
			out = append(out, fmt.Sprintf("%s 期望 %v，得到 %v", sub, want[k], gv))
		}
	}
	return out
}

func normalizeAny(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

// Explain 渲染一个样例请求，凭据一律脱敏。声明式最难查的就是"我这份描述
// 符最终发出去的到底是什么"，有了它不用抓包。
func (d *Descriptor) Explain(req Request) (string, error) {
	built, err := d.BuildComplete(req)
	if err != nil {
		return "", err
	}
	view := map[string]any{
		"method":  built.Method,
		"url":     strings.TrimSuffix(d.BaseURL, "/") + built.Path,
		"headers": maskedHeaders(built.Headers, d),
		"query":   built.Query,
		"body":    built.Body,
	}
	raw, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func maskedHeaders(headers map[string]string, d *Descriptor) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	switch d.Auth.Driver {
	case "bearer":
		name := d.Auth.Header
		if name == "" {
			name = "Authorization"
		}
		out[name] = "Bearer ***"
	case "header":
		out[d.Auth.Header] = "***"
	}
	return out
}
