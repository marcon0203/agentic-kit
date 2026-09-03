package descriptor

import (
	"encoding/json"
	"fmt"
)

// buildScope 组装表达式的变量表。这里是"secret 不进表达式"这条约束的落
// 地点：Request.Cred 由调用方保证只含非密字段，这里原样放进 $.cred。
//
// max_tokens / temperature 为零值时**不放进变量表**，这样 $.max_tokens
// 取到 nil，由 eval 的 omitempty 自然丢掉。零值和"没设置"在这一层就分开
// 了，模板里不用写条件。
func (d *Descriptor) buildScope(req Request, system string, messages []any) map[string]any {
	vars := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   req.Stream,
		"options":  anyMap(req.Options),
		"cred":     stringMapToAny(req.Cred),
	}
	if system != "" {
		vars["system"] = system
	}
	if tools := d.renderTools(req.Tools); len(tools) > 0 {
		vars["tools"] = tools
	}
	if req.MaxTokens > 0 {
		vars["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		vars["temperature"] = req.Temperature
	}
	return vars
}

// BuildComplete 渲染非流式请求。
func (d *Descriptor) BuildComplete(req Request) (HTTPRequest, error) {
	if d.Complete == nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 没有 complete 段", d.ID)
	}
	return d.buildOperation(*d.Complete, req, nil)
}

// BuildStream 渲染流式请求：和非流式共享同一个 body 模板，只把
// stream.request.patch 浅合并上去。共享模板不是为了少写几行，是为了两条
// 路径不会各自漂移——参数在一处改，两边同时生效。
func (d *Descriptor) BuildStream(req Request) (HTTPRequest, error) {
	if d.Complete == nil || d.Stream == nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 没有 stream 段", d.ID)
	}
	req.Stream = true
	return d.buildOperation(*d.Complete, req, d.Stream.Request.Patch)
}

func (d *Descriptor) buildOperation(op Operation, req Request, patch map[string]any) (HTTPRequest, error) {
	messages, system, err := d.renderMessages(req.Messages)
	if err != nil {
		return HTTPRequest{}, err
	}
	vars := d.buildScope(req, system, messages)
	s := newScope(vars)

	bodyVal, err := eval(map[string]any(op.Body), s)
	if err != nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 渲染 body: %w", d.ID, err)
	}
	body, _ := unwrap(bodyVal).(map[string]any)
	if body == nil {
		body = map[string]any{}
	}
	if len(patch) > 0 {
		patched, err := eval(map[string]any(patch), s)
		if err != nil {
			return HTTPRequest{}, fmt.Errorf("descriptor %s: 渲染 stream.request.patch: %w", d.ID, err)
		}
		if m, ok := unwrap(patched).(map[string]any); ok {
			for k, v := range m {
				body[k] = v
			}
		}
	}

	headers, err := renderStringMap(op.Headers, s)
	if err != nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 渲染 headers: %w", d.ID, err)
	}
	query, err := renderStringMap(op.Query, s)
	if err != nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 渲染 query: %w", d.ID, err)
	}
	return HTTPRequest{Method: op.Method, Path: op.Path, Headers: headers, Query: query, Body: body}, nil
}

// BuildEmbed 渲染向量化请求。变量表只有 model 和 texts——向量化不涉及消
// 息、工具和采样参数。
func (d *Descriptor) BuildEmbed(model string, texts []string) (HTTPRequest, error) {
	if d.Embed == nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 没有 embed 段", d.ID)
	}
	items := make([]any, len(texts))
	for i, t := range texts {
		items[i] = t
	}
	s := newScope(map[string]any{"model": model, "texts": items})
	bodyVal, err := eval(map[string]any(d.Embed.Body), s)
	if err != nil {
		return HTTPRequest{}, fmt.Errorf("descriptor %s: 渲染 embed.body: %w", d.ID, err)
	}
	body, _ := unwrap(bodyVal).(map[string]any)
	headers, err := renderStringMap(d.Embed.Headers, s)
	if err != nil {
		return HTTPRequest{}, err
	}
	method := d.Embed.Method
	if method == "" {
		method = "POST"
	}
	return HTTPRequest{Method: method, Path: d.Embed.Path, Headers: headers, Body: body}, nil
}

// ParseEmbed 解析向量化响应。
func (d *Descriptor) ParseEmbed(data []byte) ([][]float32, error) {
	if d.Embed == nil {
		return nil, fmt.Errorf("descriptor %s: 没有 embed 段", d.ID)
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("descriptor %s: 解析 embed 响应: %w", d.ID, err)
	}
	rows, _ := lookup(payload, d.Embed.Response.Each).([]any)
	out := make([][]float32, 0, len(rows))
	for _, row := range rows {
		raw, _ := lookup(row, d.Embed.Response.Vector).([]any)
		vec := make([]float32, len(raw))
		for i, v := range raw {
			if f, ok := v.(float64); ok {
				vec[i] = float32(f)
			}
		}
		out = append(out, vec)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("descriptor %s: embed 响应里没有向量", d.ID)
	}
	return out, nil
}

// ParseComplete 把非流式响应映射成统一结果。
func (d *Descriptor) ParseComplete(data []byte) (Result, error) {
	if d.Complete == nil {
		return Result{}, fmt.Errorf("descriptor %s: 没有 complete 段", d.ID)
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return Result{}, fmt.Errorf("descriptor %s: 解析响应: %w", d.ID, err)
	}
	m := d.Complete.Response

	// 业务错误先判：HTTP 200 里夹着 error 对象的厂商不少，当成成功往下
	// 解析会得到一个空 content 的"成功"结果，比直接报错难查得多。
	if m.Error != nil && m.Error.When != "" && lookup(payload, m.Error.When) != nil {
		msg := toString(lookup(payload, m.Error.Message))
		code := toString(lookup(payload, m.Error.Code))
		if msg == "" {
			msg = "上游返回了错误"
		}
		if code != "" {
			msg = code + ": " + msg
		}
		return Result{}, fmt.Errorf("descriptor %s: %s", d.ID, msg)
	}

	var result Result
	result.Content = firstString(payload, m.Text)
	result.Reasoning = firstString(payload, m.Reasoning)
	if m.Usage != nil {
		result.InputTokens = toInt64(lookup(payload, m.Usage.Input))
		result.OutputTokens = toInt64(lookup(payload, m.Usage.Output))
	}
	if m.ToolCalls != nil {
		calls, err := parseToolCalls(payload, m.ToolCalls)
		if err != nil {
			return Result{}, fmt.Errorf("descriptor %s: %w", d.ID, err)
		}
		result.ToolCalls = calls
	}
	return result, nil
}

func parseToolCalls(payload any, m *ToolCallsMap) ([]ToolCall, error) {
	rows, _ := lookup(payload, m.Each).([]any)
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]ToolCall, 0, len(rows))
	for _, row := range rows {
		call := ToolCall{
			ID:   toString(lookup(row, m.ID)),
			Name: toString(lookup(row, m.Name)),
		}
		// 没有名字的不是工具调用。Anthropic 的 content[] 里文本块和
		// tool_use 块混在一起，each 会把两种都遍历到——不挡的话每条带文字
		// 的回复都会多出一个空工具调用，模型下一轮会试着去"执行"它。
		//
		// 流式那条路早就是这么判的（见 stream.go 的 slot 收口），这里跟上，
		// 免得同一个响应走两条路得出不同结果。
		if call.Name == "" {
			continue
		}
		switch {
		case m.ArgumentsJSON != "":
			raw := toString(lookup(row, m.ArgumentsJSON))
			args, err := decodeArgs(raw)
			if err != nil {
				return nil, fmt.Errorf("工具调用 %q 的参数不是合法 JSON: %w", call.Name, err)
			}
			call.Arguments = args
		case m.Arguments != "":
			if obj, ok := lookup(row, m.Arguments).(map[string]any); ok {
				call.Arguments = obj
			}
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		out = append(out, call)
	}
	return out, nil
}

// decodeArgs 解析工具调用参数。空串按空对象处理——不带参数的工具，多数
// 厂商给的是 ""，那不是错误。
func decodeArgs(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

// firstString 依次尝试候选路径，返回第一个非空的。同一家厂商不同模型把
// 内容放在不同位置是常态，候选列表比在描述符里写 $coalesce 直观。
func firstString(payload any, paths []string) string {
	for _, p := range paths {
		if v := toString(lookup(payload, p)); v != "" {
			return v
		}
	}
	return ""
}

func renderStringMap(src map[string]string, s *scope) (map[string]string, error) {
	if len(src) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(src))
	for k, tmpl := range src {
		v, err := eval(tmpl, s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		if str := toString(v); str != "" {
			out[k] = str
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func anyMap(m map[string]any) any {
	if len(m) == 0 {
		return map[string]any{}
	}
	return m
}

func stringMapToAny(m map[string]string) any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
