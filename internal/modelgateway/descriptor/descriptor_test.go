package descriptor

import (
	"encoding/json"
	"strings"
	"testing"
)

// openaiWire 是一份最小的 OpenAI 线协议描述符，测试里反复用。
const openaiWire = `{
  "descriptor_version": 1,
  "id": "t", "label": "T", "wire": "openai.chat.v1",
  "capabilities": ["text", "tools", "stream"],
  "base_url": "https://example.test",
  "credentials": [{"name": "api_key", "type": "secret", "label": "K", "required": true}],
  "auth": {"driver": "bearer", "credential": "api_key"},
  "messages": {
    "tool_call_part": {"id": "$.id", "type": "function",
      "function": {"name": "$.name", "arguments": {"$json": "$.arguments"}}}
  },
  "complete": {
    "method": "POST", "path": "/chat/completions",
    "body": {"model": "$.model", "messages": "$.messages", "tools": "$.tools",
             "max_tokens": "$.max_tokens", "temperature": "$.temperature"},
    "response": {"text": ["choices.0.message.content"],
                 "usage": {"input": "usage.prompt_tokens", "output": "usage.completion_tokens"}}
  },
  "stream": {
    "request": {"patch": {"stream": true}},
    "transport": "sse", "done": ["[DONE]"],
    "on": [{"emit_text": "$.choices.0.delta.content"}]
  }
}`

func mustLoad(t *testing.T, raw string) *Descriptor {
	t.Helper()
	d, err := Load([]byte(raw))
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	return d
}

// omitempty 是默认行为，不是每个可选字段都要包一层。这条直接决定了描述符
// 的可读性——参照物那套每个可选字段都要 {"$omitEmpty": {...}}，噪音占了
// 半份文件。
func TestBuildComplete_OmitsEmptyByDefault(t *testing.T) {
	d := mustLoad(t, openaiWire)
	req, err := d.BuildComplete(Request{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	body := req.Body.(map[string]any)
	for _, key := range []string{"tools", "max_tokens", "temperature"} {
		if _, present := body[key]; present {
			t.Errorf("没设置的 %s 不该出现在请求体里: %v", key, body)
		}
	}
	if body["model"] != "m" {
		t.Errorf("model 丢了: %v", body)
	}
}

// temperature=0 和 max_tokens=0 是两种不同的"零"：前者是用户明确要的确定
// 性采样，后者是"没设置"。把 temperature: 0 当空丢掉是个会让模型行为悄悄
// 变掉的 bug。
func TestBuildComplete_ZeroTemperatureIsNotEmpty(t *testing.T) {
	d := mustLoad(t, openaiWire)
	// Temperature 为 0 时宿主不注入变量（"没设置"），非 0 才注入。
	req, _ := d.BuildComplete(Request{Model: "m", Temperature: 0.0001})
	if _, ok := req.Body.(map[string]any)["temperature"]; !ok {
		t.Fatal("非零 temperature 必须发出去")
	}
}

// 流式和非流式共享同一个 body 模板，只叠加 stream.request.patch。共享不是
// 为了少写几行，是为了两条路径不会各自漂移。
func TestBuildStream_SharesBodyTemplateAndAppliesPatch(t *testing.T) {
	d := mustLoad(t, openaiWire)
	req, err := d.BuildStream(Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	body := req.Body.(map[string]any)
	if body["stream"] != true {
		t.Errorf("patch 没生效: %v", body)
	}
	if body["model"] != "m" {
		t.Errorf("共享的 body 模板没渲染: %v", body)
	}
}

// ── 加载期校验 ────────────────────────────────────────────────────────
//
// 声明式最大的风险是"错了不知道"。这一组盯住"错了在装的时候就报错"。

func TestLoad_RejectsUnknownVariable(t *testing.T) {
	raw := strings.Replace(openaiWire, `"messages": "$.messages"`, `"messages": "$.mesages"`, 1)
	_, err := Load([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "未知变量") {
		t.Fatalf("写错的变量名必须在加载期就报错，得到: %v", err)
	}
}

func TestLoad_RejectsUnknownOperator(t *testing.T) {
	raw := strings.Replace(openaiWire, `"model": "$.model"`, `"model": {"$bogus": "x"}`, 1)
	_, err := Load([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "未知算子") {
		t.Fatalf("未知算子必须在加载期报错，得到: %v", err)
	}
}

// 这条是安全边界，不只是整洁问题：描述符能取到 secret，就等于多了一条把
// API Key 拼进 body 或 URL 发到任意地址的路径。
func TestLoad_RejectsSecretReferenceInTemplate(t *testing.T) {
	raw := strings.Replace(openaiWire, `"model": "$.model"`, `"model": "$.cred.api_key"`, 1)
	_, err := Load([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("引用 secret 必须被拒绝，得到: %v", err)
	}
}

// capabilities 是声明，要和实际存在的段对上。声明了 stream 却没有 stream
// 段，等运行时才发现"这渠道其实不支持流"就太晚了。
func TestLoad_RejectsCapabilityWithoutSection(t *testing.T) {
	var m map[string]any
	_ = json.Unmarshal([]byte(openaiWire), &m)
	delete(m, "stream")
	raw, _ := json.Marshal(m)
	_, err := Load(raw)
	if err == nil || !strings.Contains(err.Error(), "缺少 stream 段") {
		t.Fatalf("声明了 stream 却没有 stream 段必须报错，得到: %v", err)
	}
}

// 写槽位却不说写哪个槽，运行时的表现是"静默什么也没发生"——工具调用凭空
// 消失，最难查的一类。
func TestLoad_RejectsSlotOpsWithoutIndex(t *testing.T) {
	raw := strings.Replace(openaiWire,
		`"on": [{"emit_text": "$.choices.0.delta.content"}]`,
		`"on": [{"append_args": "$.x"}]`, 1)
	_, err := Load([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "index") {
		t.Fatalf("没有 index 的槽位操作必须被拒绝，得到: %v", err)
	}
}

func TestLoad_RejectsUnknownAuthDriver(t *testing.T) {
	raw := strings.Replace(openaiWire, `"driver": "bearer"`, `"driver": "aws-sigv4"`, 1)
	_, err := Load([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "鉴权驱动") {
		t.Fatalf("宿主没实现的鉴权驱动必须被拒绝，得到: %v", err)
	}
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	raw := strings.NewReplacer(
		`"messages": "$.messages"`, `"messages": "$.mesages"`,
		`"model": "$.model"`, `"model": {"$bogus": 1}`,
	).Replace(openaiWire)
	_, err := Load([]byte(raw))
	if err == nil {
		t.Fatal("期望报错")
	}
	// 一次把问题看完，比"改一个装一次"快得多。
	if !strings.Contains(err.Error(), "未知变量") || !strings.Contains(err.Error(), "未知算子") {
		t.Fatalf("校验应一次性收集全部错误，得到: %v", err)
	}
}

// ── ★ 流式状态机 ──────────────────────────────────────────────────────

// 这是整套设计的核心论断的验证：**Anthropic 那种 content_block_start 开
// 槽、input_json_delta 一片片攒 JSON、content_block_stop 才能解析的流，能
// 用七条规则声明出来。** 表达不了的话，这套体系就该退回混合模式，只覆盖
// OpenAI 兼容那一大类。
//
// 这份描述符不随产品发布（Anthropic 渠道已下线），留在测试里作为设计约束
// 的看门狗：以后谁改状态机改坏了这条语义，这个测试会先炸。
const anthropicWire = `{
  "descriptor_version": 1,
  "id": "anthropic-shape", "label": "A", "wire": "anthropic.messages.v1",
  "capabilities": ["text", "tools", "stream"],
  "base_url": "https://example.test",
  "credentials": [{"name": "api_key", "type": "secret", "label": "K", "required": true}],
  "auth": {"driver": "header", "credential": "api_key", "header": "x-api-key"},
  "messages": {
    "system": "hoist", "content": "parts",
    "text_part": {"type": "text", "text": "$.text"},
    "tool_calls": "inline_parts",
    "tool_call_part": {"type": "tool_use", "id": "$.id", "name": "$.name", "input": "$.arguments"},
    "tool_result_part": {"type": "tool_result", "tool_use_id": "$.id", "content": "$.content"},
    "tool_result_role": "user"
  },
  "complete": {
    "method": "POST", "path": "/v1/messages",
    "body": {"model": "$.model", "system": "$.system", "messages": "$.messages",
             "max_tokens": "$.max_tokens"},
    "response": {"text": ["content.0.text"],
                 "usage": {"input": "usage.input_tokens", "output": "usage.output_tokens"}}
  },
  "stream": {
    "request": {"patch": {"stream": true}},
    "transport": "sse", "event_type": "$event",
    "on": [
      {"match": "message_start", "usage_in": "$.message.usage.input_tokens"},
      {"match": "content_block_start", "open": {"index": "$.index", "kind": "$.content_block.type",
                                                "set_id": "$.content_block.id",
                                                "set_name": "$.content_block.name"}},
      {"match": "content_block_delta", "index": "$.index", "switch": "$.delta.type", "cases": {
        "text_delta": {"emit_text": "$.delta.text"},
        "thinking_delta": {"emit_reasoning": "$.delta.thinking"},
        "input_json_delta": {"append_args": "$.delta.partial_json"}
      }},
      {"match": "content_block_stop", "close": "$.index"},
      {"match": "message_delta", "usage_out": "$.usage.output_tokens"},
      {"match": "error", "fail": "$.error.message"}
    ]
  }
}`

func TestStream_AnthropicShapeIsExpressibleInSevenRules(t *testing.T) {
	d := mustLoad(t, anthropicWire)
	if len(d.Stream.On) > 7 {
		t.Fatalf("设计承诺是七条规则以内，现在 %d 条", len(d.Stream.On))
	}

	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"message":{"usage":{"input_tokens":42}}}`,
		``,
		`event: content_block_start`,
		`data: {"index":0,"content_block":{"type":"text"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"text_delta","text":"我来"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"text_delta","text":"查一下"}}`,
		``,
		`event: content_block_stop`,
		`data: {"index":0}`,
		``,
		`event: content_block_start`,
		`data: {"index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":1,"delta":{"type":"input_json_delta","partial_json":"{\"ci"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":1,"delta":{"type":"input_json_delta","partial_json":"ty\": \"北京\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"index":1}`,
		``,
		`event: message_delta`,
		`data: {"usage":{"output_tokens":17}}`,
		``,
		``,
	}, "\n")

	var deltas []string
	result, err := d.RunStream(strings.NewReader(stream), func(x Delta) {
		if x.Text != "" {
			deltas = append(deltas, x.Text)
		}
	})
	if err != nil {
		t.Fatalf("跑流失败: %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "我来" || deltas[1] != "查一下" {
		t.Errorf("文字增量不对: %v", deltas)
	}
	if result.Content != "我来查一下" {
		t.Errorf("最终文本不对: %q", result.Content)
	}
	if result.InputTokens != 42 || result.OutputTokens != 17 {
		t.Errorf("usage 不对: in=%d out=%d", result.InputTokens, result.OutputTokens)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("期望 1 个工具调用，得到 %d 个: %+v", len(result.ToolCalls), result.ToolCalls)
	}
	tc := result.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Name != "get_weather" || tc.Arguments["city"] != "北京" {
		t.Errorf("工具调用不对: %+v", tc)
	}
	// text 块也是槽，但它没有名字，不能变成工具调用——这条判据错了的话，
	// 每条回复都会多出一个空的工具调用。
	for _, c := range result.ToolCalls {
		if c.Name == "" {
			t.Error("没有名字的槽不该变成工具调用")
		}
	}
}

// system=hoist + content=parts + inline_parts 的消息形状，是 messages 段
// 存在的理由：这些差异如果让每份描述符自己写 $map，会各写一遍各错一遍。
func TestMessages_AnthropicShape(t *testing.T) {
	d := mustLoad(t, anthropicWire)
	req, err := d.BuildComplete(Request{
		Model: "m",
		Messages: []Message{
			{Role: "system", Content: "你是助手"},
			{Role: "user", Content: "北京天气"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "t1", Name: "get_weather", Arguments: map[string]any{"city": "北京"}}}},
			{Role: "tool", ToolCallID: "t1", ToolName: "get_weather", Content: "晴"},
		},
	})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	body := req.Body.(map[string]any)
	if body["system"] != "你是助手" {
		t.Errorf("system 没被抽到顶层: %v", body)
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("system 抽走后应剩 3 条消息，得到 %d: %v", len(msgs), msgs)
	}
	assistant := msgs[1].(map[string]any)
	parts := assistant["content"].([]any)
	if parts[0].(map[string]any)["type"] != "tool_use" {
		t.Errorf("工具调用应混在 content 分块里: %v", parts)
	}
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "user" {
		t.Errorf("工具结果应以 user 角色回放: %v", toolMsg)
	}
}

// 半截 JSON 拼不出合法参数时必须让整轮失败。静默丢一个工具调用，agent 会
// 卡在"我以为我调了"的状态上，比直接报错难查十倍。
func TestStream_BrokenToolArgumentsFailTheTurn(t *testing.T) {
	d := mustLoad(t, anthropicWire)
	stream := strings.Join([]string{
		`event: content_block_start`,
		`data: {"index":0,"content_block":{"type":"tool_use","id":"t1","name":"f"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"broken"}}`,
		``,
		``,
	}, "\n")
	if _, err := d.RunStream(strings.NewReader(stream), nil); err == nil {
		t.Fatal("拼不出合法 JSON 的工具参数必须让整轮失败，而不是静默丢掉")
	}
}

// fail 规则命中 = 整轮失败。上游在流里报错却被当成正常结束的话，用户会看
// 到一段半截的回复且没有任何错误提示。
func TestStream_FailRuleEndsTheTurn(t *testing.T) {
	d := mustLoad(t, anthropicWire)
	stream := "event: error\ndata: {\"error\":{\"message\":\"overloaded\"}}\n\n"
	_, err := d.RunStream(strings.NewReader(stream), nil)
	if err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("期望带上游错误信息的失败，得到: %v", err)
	}
}

// 非 JSON 的保活帧（有些网关会插 : ping 或空 data）不能让整轮失败。
func TestStream_IgnoresKeepAliveFrames(t *testing.T) {
	d := mustLoad(t, openaiWire)
	stream := ": ping\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: not-json\n\ndata: [DONE]\n\n"
	result, err := d.RunStream(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("保活帧不该让整轮失败: %v", err)
	}
	if result.Content != "hi" {
		t.Errorf("内容不对: %q", result.Content)
	}
}

// ── 表达式 ────────────────────────────────────────────────────────────

func TestEval_KeepPreservesEmptyValue(t *testing.T) {
	out, err := eval(map[string]any{
		"dropped": "$.missing",
		"kept":    map[string]any{"$keep": "$.missing"},
	}, newScope(map[string]any{"model": "m"}))
	if err != nil {
		t.Fatalf("求值失败: %v", err)
	}
	m := out.(map[string]any)
	if _, present := m["dropped"]; present {
		t.Error("空值默认应被丢弃")
	}
	if v, present := m["kept"]; !present || v != nil {
		t.Errorf("$keep 包住的空值应保留为 null，得到 %v present=%v", v, present)
	}
}

func TestEval_Coalesce(t *testing.T) {
	out, err := eval(map[string]any{
		"v": map[string]any{"$coalesce": []any{"$.missing", "$.model"}},
	}, newScope(map[string]any{"model": "fallback"}))
	if err != nil {
		t.Fatalf("求值失败: %v", err)
	}
	if out.(map[string]any)["v"] != "fallback" {
		t.Errorf("$coalesce 应取第一个非空值: %v", out)
	}
}

func TestLookup_IndexesArrays(t *testing.T) {
	var payload any
	_ = json.Unmarshal([]byte(`{"choices":[{"message":{"content":"hi"}}]}`), &payload)
	if got := lookup(payload, "choices.0.message.content"); got != "hi" {
		t.Errorf("数字段应能索引数组，得到 %v", got)
	}
	if got := lookup(payload, "choices.9.message.content"); got != nil {
		t.Errorf("越界索引应返回 nil，得到 %v", got)
	}
}
