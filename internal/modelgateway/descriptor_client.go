package modelgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/marcon0203/agentic-kit/internal/modelgateway/descriptor"
)

// descriptorClient 把一份声明式渠道描述符接成这个包的 Client /
// StreamingClient / EmbeddingClient。
//
// 分工严格照设计文档：描述符只做协议翻译（产出 HTTPRequest、消费响应字
// 节），这里负责发包、注入凭据、判 HTTP 状态。凭据在这一层——且只在这一
// 层——进入请求，描述符的表达式取不到 secret 字段。
type descriptorClient struct {
	desc       *descriptor.Descriptor
	httpClient *http.Client
	defaultURL string
}

func newDescriptorClient(d *descriptor.Descriptor, hc *http.Client, base string) *descriptorClient {
	return &descriptorClient{desc: d, httpClient: hc, defaultURL: base}
}

func (c *descriptorClient) base(baseURL string) string {
	if baseURL != "" {
		return strings.TrimRight(baseURL, "/")
	}
	return strings.TrimRight(c.defaultURL, "/")
}

// toDescriptorRequest 转成描述符包的中立类型。Cred 只带**非密**字段：
// secret 永远不进表达式作用域，这是"描述符没有任何办法把 API Key 拼进
// body 或 URL"这条保证的落地点。
func (c *descriptorClient) toDescriptorRequest(model string, req CompletionRequest, cred Credential) descriptor.Request {
	out := descriptor.Request{
		Model:       model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Options:     req.Options,
		Cred:        map[string]string{},
	}
	for name, value := range cred.Fields {
		if c.desc.IsSecret(name) {
			continue
		}
		out.Cred[name] = value
	}
	for _, m := range req.Messages {
		msg := descriptor.Message{
			Role: m.Role, Content: m.Content,
			ToolCallID: m.ToolCallID, ToolName: m.ToolName,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, descriptor.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
		}
		out.Messages = append(out.Messages, msg)
	}
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, descriptor.Tool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return out
}

func fromDescriptorResult(r descriptor.Result) CompletionResult {
	out := CompletionResult{
		Content:      r.Content,
		InputTokens:  r.InputTokens,
		OutputTokens: r.OutputTokens,
	}
	for _, tc := range r.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
	}
	return out
}

// checkRequiredParams 在请求发出去之前拦下"必填参数没配"的调用。上游的报错
// 往往是一句没有字段的 "Invalid request Error"，把它翻译成点名渠道、模型和
// 参数名的本地错误——日志里一眼知道去哪里补配置。
func (c *descriptorClient) checkRequiredParams(model string, req CompletionRequest) error {
	for _, p := range c.desc.RequestParams {
		if !p.Required {
			continue
		}
		missing := false
		switch p.Name {
		case "max_tokens":
			missing = req.MaxTokens <= 0
		case "temperature":
			// 零值在整条管道里的语义就是"未设置"（buildScope 只注入非零
			// 值），必填的 temperature 取 0 等同于没配。
			missing = req.Temperature == 0
		default:
			_, ok := req.Options[p.Name]
			missing = !ok
		}
		if missing {
			return fmt.Errorf("渠道 %s 的模型 %s 缺少必填请求参数 %s：在 系统配置 → 模型提供商 里编辑该模型补全后再调用", c.desc.ID, model, p.Name)
		}
	}
	return nil
}

func (c *descriptorClient) Complete(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest) (CompletionResult, error) {
	if err := c.checkRequiredParams(model, req); err != nil {
		return CompletionResult{}, err
	}
	spec, err := c.desc.BuildComplete(c.toDescriptorRequest(model, req, credOf(ctx, apiKey, baseURL)))
	if err != nil {
		return CompletionResult{}, err
	}
	body, status, url, err := c.do(ctx, spec, apiKey, baseURL, false)
	if err != nil {
		return CompletionResult{}, err
	}
	if status < 200 || status >= 300 {
		return CompletionResult{}, httpError(c.desc.ID, url, status, body)
	}
	result, err := c.desc.ParseComplete(body)
	if err != nil {
		return CompletionResult{}, err
	}
	return fromDescriptorResult(result), nil
}

func (c *descriptorClient) CompleteStream(ctx context.Context, apiKey, baseURL, model string, req CompletionRequest, onDelta func(StreamDelta)) (CompletionResult, error) {
	if c.desc.Stream == nil {
		// 没声明流式的渠道退回一次性调用，由 Gateway 合成单个 delta。
		return c.Complete(ctx, apiKey, baseURL, model, req)
	}
	if err := c.checkRequiredParams(model, req); err != nil {
		return CompletionResult{}, err
	}
	spec, err := c.desc.BuildStream(c.toDescriptorRequest(model, req, credOf(ctx, apiKey, baseURL)))
	if err != nil {
		return CompletionResult{}, err
	}
	httpReq, err := c.newRequest(ctx, spec, apiKey, baseURL, true)
	if err != nil {
		return CompletionResult{}, err
	}
	url := httpReq.URL.String()
	started := time.Now()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		logUpstream(c.desc.ID, httpReq.Method, url, 0, started, err)
		return CompletionResult{}, fmt.Errorf("%s: 请求 %s 失败: %w", c.desc.ID, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	logUpstream(c.desc.ID, httpReq.Method, url, resp.StatusCode, started, nil)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 错误响应不是流，一次性读完再报——否则错误信息会被当成事件帧
		// 丢掉，只剩一个光秃秃的状态码。
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return CompletionResult{}, httpError(c.desc.ID, url, resp.StatusCode, raw)
	}

	result, err := c.desc.RunStream(resp.Body, func(d descriptor.Delta) {
		if onDelta == nil {
			return
		}
		// 思维链也当文字往外推：现在 StreamDelta 只有 TextDelta 一个通
		// 道，把 reasoning 吞掉的话前端在模型思考期间是完全静止的。
		if d.Text != "" {
			onDelta(StreamDelta{TextDelta: d.Text})
		} else if d.Reasoning != "" {
			onDelta(StreamDelta{TextDelta: d.Reasoning})
		}
	})
	if err != nil {
		return CompletionResult{}, err
	}
	return fromDescriptorResult(result), nil
}

func (c *descriptorClient) Embed(ctx context.Context, apiKey, baseURL, model string, texts []string) ([][]float32, error) {
	if c.desc.Embed == nil {
		return nil, fmt.Errorf("%w: %q", ErrEmbeddingsNotSupported, c.desc.ID)
	}
	spec, err := c.desc.BuildEmbed(model, texts)
	if err != nil {
		return nil, err
	}
	body, status, url, err := c.do(ctx, spec, apiKey, baseURL, false)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, httpError(c.desc.ID, url, status, body)
	}
	return c.desc.ParseEmbed(body)
}

// do 发一次非流式请求。返回的 url 供调用方拼错误信息用——出错时没有它，
// 用户不知道请求到底打到哪去了。
func (c *descriptorClient) do(ctx context.Context, spec descriptor.HTTPRequest, apiKey, baseURL string, stream bool) (body []byte, status int, url string, err error) {
	req, err := c.newRequest(ctx, spec, apiKey, baseURL, stream)
	if err != nil {
		return nil, 0, "", err
	}
	url = req.URL.String()
	started := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logUpstream(c.desc.ID, req.Method, url, 0, started, err)
		// 连不上时把地址也带进错误：DNS 写错、内网地址不通都长这样。
		return nil, 0, url, fmt.Errorf("%s: 请求 %s 失败: %w", c.desc.ID, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	logUpstream(c.desc.ID, req.Method, url, resp.StatusCode, started, err)
	if err != nil {
		return nil, resp.StatusCode, url, err
	}
	return body, resp.StatusCode, url, nil
}

func (c *descriptorClient) newRequest(ctx context.Context, spec descriptor.HTTPRequest, apiKey, baseURL string, stream bool) (*http.Request, error) {
	base := c.base(baseURL)
	if base == "" {
		return nil, fmt.Errorf("modelgateway: 渠道 %s 没有配置接口地址", c.desc.ID)
	}
	var reader io.Reader
	if spec.Body != nil {
		raw, err := json.Marshal(spec.Body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, spec.Method, base+spec.Path, reader)
	if err != nil {
		return nil, err
	}
	if spec.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	if len(spec.Query) > 0 {
		q := req.URL.Query()
		for k, v := range spec.Query {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}
	applyDescriptorAuth(req, c.desc.Auth, apiKey)
	return req, nil
}

// applyDescriptorAuth 是凭据进入请求的**唯一**入口。驱动是宿主实现的封
// 闭集：描述符只能点名，不能实现算法。签名类（sigv4/tc3 等）要读 body 做
// 摘要，本质上不可声明式表达，是有意封口——新增一种签名 = 改 Go = 发版
// 本，可以接受，因为签名协议的新增频率远低于新增渠道。
func applyDescriptorAuth(req *http.Request, auth descriptor.Auth, apiKey string) {
	switch auth.Driver {
	case "", "none":
		return
	case "bearer":
		header := auth.Header
		if header == "" {
			header = "Authorization"
		}
		prefix := auth.Prefix
		if prefix == "" {
			prefix = "Bearer "
		}
		req.Header.Set(header, prefix+apiKey)
	case "header":
		req.Header.Set(auth.Header, auth.Prefix+apiKey)
	case "query":
		q := req.URL.Query()
		q.Set(auth.Query, auth.Prefix+apiKey)
		req.URL.RawQuery = q.Encode()
	}
}

// httpError 把上游的错误体截断后带出来。只给一个状态码的话，"模型名写错
// 了"和"key 过期了"看起来一模一样。
// httpError 把上游的失败拼成一条能直接排查的错误。
//
// 带上**完整请求地址**是有意的：这套东西最常见的故障就是 404，而 404 几
// 乎总是"接口地址或线协议选错了"（比如拿 OpenAI 模板去打一个 Anthropic 兼
// 容端点）。只报一个状态码的话，用户手里没有任何可查的线索。
//
// url 里不会有凭据——鉴权一律走请求头，描述符的表达式作用域碰不到 secret
// 字段（见 toDescriptorRequest）。
func httpError(id, url string, status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 512 {
		msg = msg[:512] + "…"
	}
	hint := ""
	if status == http.StatusNotFound {
		hint = "（404 多半是接口地址或线协议不对：确认 base_url 和这个渠道的协议模板匹配）"
	}
	if msg == "" {
		return fmt.Errorf("%s: http %d %s%s", id, status, url, hint)
	}
	return fmt.Errorf("%s: http %d %s%s: %s", id, status, url, hint, msg)
}

// logUpstream 把每次上游调用记一条结构化日志。
//
// 之前出问题时日志里连打到哪个地址都没有，只能靠猜。这里记的是方法、完整
// URL、状态码和耗时——凭据在请求头里，不在这几样里。
func logUpstream(id, method, url string, status int, started time.Time, err error) {
	attrs := []any{
		"channel", id, "method", method, "url", url,
		"elapsed_ms", time.Since(started).Milliseconds(),
	}
	switch {
	case err != nil:
		slog.Error("model_upstream_call_failed", append(attrs, "err", err)...)
	case status < 200 || status >= 300:
		slog.Warn("model_upstream_call_non_2xx", append(attrs, "status", status)...)
	default:
		slog.Debug("model_upstream_call", append(attrs, "status", status)...)
	}
}

// credOf 只是把 Client 接口那两个位置参数收拢成 Credential——Client 的签
// 名是历史形状，改它要动所有实现和调用点，不值得。ctx 目前没用到，留着
// 是为了以后把整份 Credential 顺着 ctx 传下来时不用再改签名。
func credOf(_ context.Context, apiKey, baseURL string) Credential {
	return Credential{APIKey: apiKey, BaseURL: baseURL}
}

// descriptorValidator 是登记凭据时的连通性校验，走描述符的 probe 段。
type descriptorValidator struct {
	desc       *descriptor.Descriptor
	httpClient *http.Client
	baseURL    string
}

func (v *descriptorValidator) Validate(ctx context.Context, apiKey string) error {
	if v.desc.Probe == nil {
		// 没声明 probe 的渠道不做联网校验：拒绝登记比放行更糟，用户会
		// 卡在一个自己无从解决的报错上。
		return nil
	}
	if v.baseURL == "" {
		return &validationError{"no base_url configured for this provider"}
	}
	method := v.desc.Probe.Method
	if method == "" {
		method = http.MethodGet
	}
	target, err := url.JoinPath(strings.TrimRight(v.baseURL, "/"), v.desc.Probe.Path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return err
	}
	applyDescriptorAuth(req, v.desc.Auth, apiKey)
	return doAuthProbe(v.httpClient, req)
}
