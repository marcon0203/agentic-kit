package modelgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcon0203/agentic-kit/internal/channeltemplates"
	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
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

// 平台开箱不带渠道；这几个是 TestMain 按协议模板建出来的，等价于管理员在
// 系统配置 → 模型提供商 里建了它们。
func TestProviderNames_MatchesTheTestChannels(t *testing.T) {
	want := map[string]bool{
		"deepseek": true, "volcengine": true, "qwen": true, "zhipu": true, "custom": true, "kimi": true,
	}
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

// 智谱的接口前缀是 /api/paas/v4，不是 /v1。这是接智谱最常见的一个坑——很
// 多客户端会自作主张往 base_url 后面拼 /v1，于是一路 404。这条盯住模板里
// 的 base_url 原样进请求、路径就是 base_url + /chat/completions。
func TestZhipu_UsesTheV4PrefixVerbatim(t *testing.T) {
	def, ok := providerByName("zhipu")
	if !ok {
		t.Fatal("智谱渠道没注册")
	}
	if def.DefaultBaseURL != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("默认接口地址不对（智谱用 /api/paas/v4 而不是 /v1）: %s", def.DefaultBaseURL)
	}

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"你好"}}],
		                       "usage":{"prompt_tokens":5,"completion_tokens":2}}`))
	}))
	defer srv.Close()

	result, err := def.NewClient(srv.Client(), srv.URL).
		Complete(context.Background(), "sk-zhipu", "", "glm-4.6", CompletionRequest{
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("路径不对: %s", gotPath)
	}
	// 智谱的 v4 接口直接收 API Key 作 bearer，不需要 JWT 签名——所以它能走
	// 现成的 bearer 驱动，不必为它开一个签名驱动。
	if gotAuth != "Bearer sk-zhipu" {
		t.Errorf("鉴权头不对: %q", gotAuth)
	}
	if result.Content != "你好" || result.InputTokens != 5 || result.OutputTokens != 2 {
		t.Errorf("结果不对: %+v", result)
	}
}

// GLM 的思考型号会额外返回 reasoning_content，这条盯住它被怎么处理：思维
// 链走 StreamDelta.ReasoningDelta 这条独立通道（不混进 TextDelta），最终
// 结果里 Content 只是答案、思维链归 Result.Reasoning 槽——否则它会被当成
// 正文写进对话记录、并跟着进下一轮上下文。
func TestZhipu_StreamsReasoningOnItsOwnChannelAndKeepsItOutOfTheFinalContent(t *testing.T) {
	def, _ := providerByName("zhipu")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, chunk := range []string{
			`{"choices":[{"delta":{"reasoning_content":"先想一下"}}]}`,
			`{"choices":[{"delta":{"content":"答案是"}}]}`,
			`{"choices":[{"delta":{"content":"42"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`,
			`[DONE]`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
		}
	}))
	defer srv.Close()

	sc, ok := def.NewClient(srv.Client(), srv.URL).(StreamingClient)
	if !ok {
		t.Fatal("智谱渠道必须实现 StreamingClient")
	}
	var reasoned, streamed string
	result, err := sc.CompleteStream(context.Background(), "sk-zhipu", "", "glm-4.6",
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}},
		func(d StreamDelta) {
			reasoned += d.ReasoningDelta
			streamed += d.TextDelta
		})
	if err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	// 思维链只能从 ReasoningDelta 冒出来，不能混进 TextDelta。
	if reasoned != "先想一下" {
		t.Errorf("思维链应该从 ReasoningDelta 单独推出去: %q", reasoned)
	}
	if strings.Contains(streamed, "先想一下") {
		t.Errorf("思维链不该混进 TextDelta: %q", streamed)
	}
	if streamed != "答案是42" {
		t.Errorf("TextDelta 应该只有答案: %q", streamed)
	}
	// 最终正文里也不该有思维链——那会被写进对话记录并带进下一轮上下文。
	if result.Content != "答案是42" {
		t.Errorf("最终正文里不该有思维链: %q", result.Content)
	}
	if result.Reasoning != "先想一下" {
		t.Errorf("思维链应该落进 Result.Reasoning: %q", result.Reasoning)
	}
	if result.InputTokens != 3 || result.OutputTokens != 4 {
		t.Errorf("用量不对: %+v", result)
	}
}

// 与火山方舟同样的理由：GLM 按人民币计价且调价频繁，刻意不预置价格表，成
// 本按 0 记而不是记一个折算出来的假数字。
func TestZhipu_HasNoGuessedPricing(t *testing.T) {
	if cost := EstimateCost("zhipu", "glm-4.6", 1_000_000, 1_000_000); cost != 0 {
		t.Fatalf("没有价格表的渠道应按 0 计费，得到 %v", cost)
	}
}

// 404 是这套东西最常见的故障，而它几乎总是"接口地址或线协议选错了"——比
// 如拿 OpenAI 模板去打一个 Anthropic 兼容端点。只报一个状态码的话，用户手
// 里没有任何可查的线索，所以错误里必须带上完整请求地址和那句提示。
func TestHTTPError_CarriesTheRequestURLAndA404Hint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	_, err := deepSeekClient(t, srv).Complete(context.Background(), "sk", "", "m", CompletionRequest{})
	if err == nil {
		t.Fatal("404 应该报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, srv.URL+gotPath) {
		t.Errorf("错误里必须带完整请求地址，得到: %s", msg)
	}
	if !strings.Contains(msg, "线协议") {
		t.Errorf("404 要提示可能是地址或协议不对，得到: %s", msg)
	}
	if !strings.Contains(msg, "not found") {
		t.Errorf("上游正文也要带出来，得到: %s", msg)
	}
}

// 连不上（DNS 写错、内网地址不通）时同样要说清楚打的是哪个地址。
func TestUpstreamUnreachable_ErrorNamesTheURL(t *testing.T) {
	def, _ := providerByName("deepseek")
	// 指向一个不会有人监听的地址。
	client := def.NewClient(&http.Client{Timeout: 2 * time.Second}, "http://127.0.0.1:1")

	_, err := client.Complete(context.Background(), "sk", "", "m", CompletionRequest{})
	if err == nil {
		t.Fatal("连不上应该报错")
	}
	if !strings.Contains(err.Error(), "http://127.0.0.1:1/chat/completions") {
		t.Errorf("连不上时也要带上地址，得到: %s", err)
	}
}

// 拿 OpenAI 模板去接一个 Anthropic 兼容端点，就是用户遇到的那个 404：请求
// 打到 /chat/completions，而对方只有 /v1/messages。这条把"选对模板就打对
// 路径"钉住——两个模板同一个 base_url，路径必须不同。
func TestAnthropicAndOpenAITemplates_HitDifferentPaths(t *testing.T) {
	for _, tc := range []struct {
		template, key, wantPath string
	}{
		{"openai-compatible", "oa", "/chat/completions"},
		{"anthropic-messages", "an", "/v1/messages"},
		{"kimi-for-coding", "kimi", "/v1/messages"},
		{"openai-responses", "resp", "/responses"},
	} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			// 三套线协议各自能解析的最小成功响应。
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],
			                       "content":[{"type":"text","text":"ok"}],
			                       "output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
		}))

		d, _, err := channeltemplates.Instantiate(tc.template, tc.key, tc.key, srv.URL)
		if err != nil {
			srv.Close()
			t.Fatalf("模板 %s 实例化失败: %v", tc.template, err)
		}
		SetChannels([]*descriptor.Descriptor{d})
		def, ok := providerByName(tc.key)
		if !ok {
			srv.Close()
			t.Fatalf("渠道 %s 没注册", tc.key)
		}
		_, err = def.NewClient(srv.Client(), srv.URL).
			Complete(context.Background(), "k", "", "m", CompletionRequest{
				// Anthropic 线协议必填 max_tokens（见 request_params）——
				// 这里给所有线协议统一带上，路径断言不受影响。
				Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 64,
			})
		srv.Close()
		if err != nil {
			t.Errorf("%s 调用失败: %v", tc.template, err)
		}
		if gotPath != tc.wantPath {
			t.Errorf("%s 应该打 %s，实际打了 %s", tc.template, tc.wantPath, gotPath)
		}
	}
	restoreTestChannels(t)
}

// Kimi 那个地址（https://api.kimi.com/coding）后面没有 /v1，要靠模板补上。
// 用户填的地址原样拼接、不多不少，这是 404 与否的分界。
func TestKimiTemplate_AppendsV1MessagesToTheGivenBase(t *testing.T) {
	d, _, err := channeltemplates.Instantiate("kimi-for-coding", "kimi", "Kimi", "")
	if err != nil {
		t.Fatalf("实例化失败: %v", err)
	}
	if d.BaseURL != "https://api.kimi.com/coding" {
		t.Errorf("默认地址不对: %s", d.BaseURL)
	}
	if d.Complete.Path != "/v1/messages" {
		t.Errorf("路径应为 /v1/messages，得到 %s", d.Complete.Path)
	}
	// Anthropic 兼容端点里 Kimi 用的是 bearer 而不是官方的 x-api-key。
	if d.Auth.Driver != "bearer" {
		t.Errorf("Kimi 的鉴权应为 bearer，得到 %s", d.Auth.Driver)
	}
}

// ── 模型级请求参数 ─────────────────────────────────────────────────────

// 模型参数在 Gateway 层按 provider+model 注入。这条走完整链路（注册表 →
// Gateway → descriptorClient → httptest 上游），盯的是"没设置的参数被模型
// 参数补上、显式设置的值优先"。
func TestGateway_InjectsModelParamsIntoTheRequest(t *testing.T) {
	var gotBody map[string]any
	var bodyMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 每个请求新建 map：json.Decode 往已有 map 里是合并而不是清空，
		// 复用会把上一个请求的字段带进断言。
		fresh := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&fresh)
		bodyMu.Lock()
		gotBody = fresh
		bodyMu.Unlock()
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	override := map[string]map[string]map[string]any{
		"deepseek": {
			"deepseek-chat": {"max_tokens": 4096, "temperature": 0.3},
		},
	}
	SetModelParams(override)
	t.Cleanup(func() {
		SetModelParams(nil)
		restoreTestChannels(t)
	})
	// 把渠道指到测试服务器上（Gateway 用描述符里的 base_url 建连接）。
	SetChannels([]*descriptor.Descriptor{mustInstantiate(t, "deepseek", "deepseek", srv.URL)})

	gw := NewGateway(nil)
	// 请求里没设 max_tokens/temperature → 用模型参数补。
	if _, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil,
		map[string]Credential{"deepseek": {APIKey: "sk"}},
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	body := func() map[string]any {
		bodyMu.Lock()
		defer bodyMu.Unlock()
		return gotBody
	}
	if body()["max_tokens"] != float64(4096) || body()["temperature"] != 0.3 {
		t.Errorf("模型参数应注入请求体: %v", gotBody)
	}

	// 请求里显式给了值 → 显式值优先，模型参数不覆盖。
	if _, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "deepseek", Name: "deepseek-chat"}, nil,
		map[string]Credential{"deepseek": {APIKey: "sk"}},
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 64}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if body()["max_tokens"] != float64(64) {
		t.Errorf("显式请求值应优先于模型参数: %v", gotBody)
	}

	// 没配参数的模型（另一个模型名）原样放行，不会被别的模型的参数污染。
	if _, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "deepseek", Name: "deepseek-r1"}, nil,
		map[string]Credential{"deepseek": {APIKey: "sk"}},
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if _, present := body()["max_tokens"]; present {
		t.Errorf("没配参数的模型不该带上别的模型的参数: %v", gotBody)
	}
}

// Anthropic 线协议的 max_tokens 是必填项。模型没配参数时，请求必须在发出去
// 之前被拦下，错误点名渠道、模型和参数——而不是等上游回一句没有字段的
// "Invalid request Error"。
func TestGateway_MissingRequiredParamFailsBeforeHTTP(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	defer srv.Close()

	SetModelParams(nil)
	t.Cleanup(func() {
		SetModelParams(nil)
		restoreTestChannels(t)
	})
	SetChannels([]*descriptor.Descriptor{mustInstantiate(t, "kimi-for-coding", "kimi", srv.URL)})

	gw := NewGateway(nil)
	_, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "kimi", Name: "k3"}, nil,
		map[string]Credential{"kimi": {APIKey: "sk"}},
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("缺必填参数的调用必须报错")
	}
	for _, want := range []string{"kimi", "k3", "max_tokens"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误里应点名 %s，得到: %v", want, err)
		}
	}
	if requests != 0 {
		t.Errorf("缺必填参数时不应发出 HTTP 请求，实际发了 %d 次", requests)
	}

	// 配上参数后同一个渠道立刻能用：拦的是"缺参数"，不是渠道本身。
	SetModelParams(map[string]map[string]map[string]any{
		"kimi": {"k3": {"max_tokens": 8192}},
	})
	var gotBody map[string]any
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	})
	result, err := gw.Complete(context.Background(),
		ModelSpec{Provider: "kimi", Name: "k3"}, nil,
		map[string]Credential{"kimi": {APIKey: "sk"}},
		CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("配了参数后应该能调通: %v", err)
	}
	if result.Content != "ok" || gotBody["max_tokens"] != float64(8192) {
		t.Errorf("参数注入后的请求不对: %v / %+v", gotBody, result)
	}
}

// mustInstantiate 按模板实例化一个指向测试服务器的渠道。
func mustInstantiate(t *testing.T, template, key, baseURL string) *descriptor.Descriptor {
	t.Helper()
	d, _, err := channeltemplates.Instantiate(template, key, key, baseURL)
	if err != nil {
		t.Fatalf("模板 %s 实例化失败: %v", template, err)
	}
	return d
}
