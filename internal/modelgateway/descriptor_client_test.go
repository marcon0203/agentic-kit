package modelgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 这一组走的是真实链路：内置的 deepseek 描述符 → descriptorClient → 一个
// httptest 上游。它盯的是描述符包的单测覆盖不到的那一半——凭据注入、HTTP
// 状态判定、流式响应体的消费。

func deepSeekClient(t *testing.T, srv *httptest.Server) Client {
	t.Helper()
	def, ok := providerByName("deepseek")
	if !ok {
		t.Fatal("deepseek 渠道没注册")
	}
	return def.NewClient(srv.Client(), srv.URL)
}

func TestDescriptorClient_Complete(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}],
		                        "usage":{"prompt_tokens":8,"completion_tokens":4}}`))
	}))
	defer srv.Close()

	result, err := deepSeekClient(t, srv).Complete(context.Background(), "sk-test", "", "deepseek-chat",
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 64})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("路径不对: %s", gotPath)
	}
	// 凭据只经鉴权驱动进入请求，描述符的表达式碰不到它。
	if gotAuth != "Bearer sk-test" {
		t.Errorf("鉴权头不对: %q", gotAuth)
	}
	if gotBody["model"] != "deepseek-chat" || gotBody["max_tokens"] != float64(64) {
		t.Errorf("请求体不对: %v", gotBody)
	}
	if result.Content != "你好" || result.InputTokens != 8 || result.OutputTokens != 4 {
		t.Errorf("结果不对: %+v", result)
	}
}

func TestDescriptorClient_CompleteStream_EmitsIncrementalDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("流式请求必须带 stream=true: %v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []string{
			`{"choices":[{"delta":{"content":"你"}}]}`,
			`{"choices":[{"delta":{"content":"好"}}]}`,
			`{"choices":[{"delta":{}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
			`[DONE]`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
		}
	}))
	defer srv.Close()

	sc, ok := deepSeekClient(t, srv).(StreamingClient)
	if !ok {
		t.Fatal("描述符渠道必须实现 StreamingClient")
	}
	var deltas []string
	result, err := sc.CompleteStream(context.Background(), "sk-test", "", "deepseek-chat",
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}},
		func(d StreamDelta) { deltas = append(deltas, d.TextDelta) })
	if err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "你" || deltas[1] != "好" {
		t.Errorf("期望两个增量，得到 %v", deltas)
	}
	if result.Content != "你好" || result.InputTokens != 3 || result.OutputTokens != 2 {
		t.Errorf("最终结果不对: %+v", result)
	}
}

// 工具调用在流里分片到达：id 和 name 只在第一片，参数一个片段一个片段地
// 攒。后续片段的空 name 不能把已有的 name 冲掉。
func TestDescriptorClient_CompleteStream_AssemblesToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, chunk := range []string{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"北京\"}"}}]}}]}`,
			`[DONE]`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
		}
	}))
	defer srv.Close()

	sc := deepSeekClient(t, srv).(StreamingClient)
	var deltas []string
	result, err := sc.CompleteStream(context.Background(), "sk-test", "", "deepseek-chat",
		CompletionRequest{Messages: []Message{{Role: "user", Content: "天气"}}},
		func(d StreamDelta) { deltas = append(deltas, d.TextDelta) })
	if err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	// 半截 JSON 不该被推给前端——显示不了也执行不了。
	if len(deltas) != 0 {
		t.Errorf("工具调用参数不该作为文字增量推出去: %v", deltas)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("期望 1 个工具调用，得到 %+v", result.ToolCalls)
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "get_weather" || tc.Arguments["city"] != "北京" {
		t.Errorf("工具调用拼装不对: %+v", tc)
	}
}

// 上游报错时，只回一个状态码的话，"模型名写错了"和"key 过期了"看起来一模
// 一样。错误体必须带出来。
func TestDescriptorClient_SurfacesUpstreamErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Model Not Exist"}}`))
	}))
	defer srv.Close()

	_, err := deepSeekClient(t, srv).Complete(context.Background(), "sk-test", "", "nope", CompletionRequest{})
	if err == nil || !strings.Contains(err.Error(), "Model Not Exist") {
		t.Fatalf("上游的错误信息必须带出来，得到: %v", err)
	}
}

// 流式失败时上游回的是普通 JSON 而不是事件流，按事件流解析会把错误信息整
// 个丢掉，只剩一个光秃秃的状态码。
func TestDescriptorClient_StreamErrorResponseIsNotParsedAsStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Authentication Fails"}}`))
	}))
	defer srv.Close()

	sc := deepSeekClient(t, srv).(StreamingClient)
	_, err := sc.CompleteStream(context.Background(), "bad", "", "deepseek-chat", CompletionRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "Authentication Fails") {
		t.Fatalf("流式的错误响应也必须带出错误信息，得到: %v", err)
	}
}

// 火山方舟和 deepseek 说的是同一套线协议（wire: openai.chat.v1），共用一
// 份 fixtures。这条盯住"新增一个同族渠道 = 换个 base_url，不写一行 Go"。
func TestVolcengine_IsRegisteredAndSpeaksTheSameWire(t *testing.T) {
	def, ok := providerByName("volcengine")
	if !ok {
		t.Fatal("火山引擎渠道没注册")
	}
	if def.DefaultBaseURL == "" {
		t.Error("火山引擎应有默认接口地址")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("路径不对: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	result, err := def.NewClient(srv.Client(), srv.URL).
		Complete(context.Background(), "k", "", "ep-20240101", CompletionRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
	if err != nil || result.Content != "ok" {
		t.Fatalf("调用失败: %v / %+v", err, result)
	}
}

// 描述符自带价格表，EstimateCost 读的是同一张表——加渠道不用再改 Go 侧的
// 第二份价格表。火山方舟按人民币计价且随接入点档位变化，刻意不预置价格，
// 成本按 0 记而不是记一个折算出来的假数字。
func TestVolcengine_HasNoGuessedPricing(t *testing.T) {
	if cost := EstimateCost("volcengine", "ep-20240101", 1_000_000, 1_000_000); cost != 0 {
		t.Fatalf("没有价格表的渠道应按 0 计费，得到 %v", cost)
	}
}

// ProviderSpecs 是前端渲染凭据表单的唯一来源，前端不再抄第二份。
func TestProviderSpecs_DescribeCredentialFields(t *testing.T) {
	for _, spec := range ProviderSpecs() {
		if spec.Label == "" {
			t.Errorf("渠道 %s 缺少显示名", spec.Name)
		}
		if len(spec.Credentials) == 0 {
			t.Errorf("渠道 %s 没有声明任何凭据字段", spec.Name)
		}
	}
}

func TestProviderNames_MatchesTheShippedChannels(t *testing.T) {
	want := map[string]bool{"deepseek": true, "volcengine": true, "qwen": true, "custom": true, "google": true}
	got := ProviderNames()
	if len(got) != len(want) {
		t.Fatalf("期望 %d 个渠道，得到 %d 个: %v", len(want), len(got), got)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("意料之外的渠道 %q", name)
		}
	}
}
