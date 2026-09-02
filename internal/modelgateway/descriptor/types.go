// Package descriptor implements 模型渠道描述符（Model Channel Descriptor）：
// 一份 JSON 描述"怎么把统一的对话请求翻译成某家厂商的线协议"，由这里的
// 解释器执行。设计文档见 docs/模型渠道插件体系设计.md。
//
// 边界（照抄不误的一条）：描述符只做协议翻译。它产出一个 HTTPRequest，由
// 宿主发包；它消费响应字节，产出统一结果。它不持有凭据、不发 HTTP、不重
// 试、不计费——那些是 modelgateway.Gateway 的事。
//
// 这个包刻意不 import modelgateway：那边要 import 这里来注册渠道，反向依
// 赖会成环。代价是下面这几个类型和 modelgateway 的同名类型重复一遍，由
// modelgateway/descriptor_client.go 做一层薄转换。
package descriptor

// Message 是一轮对话，与厂商无关。字段语义和 modelgateway.Message 一致：
// Role 为 "assistant" 且 ToolCalls 非空 = 模型决定调工具；Role 为 "tool"
// 且 ToolCallID 非空 = 那次调用的结果回放。
type Message struct {
	Role       string     `json:"role,omitempty"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}

// ToolCall 是一次工具调用。Arguments 已经是解析后的对象——描述符里用
// arguments_json 声明"上游给的是 JSON 字符串"，解析在解释器里做。
type ToolCall struct {
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// Tool 是暴露给模型的一个工具定义。
type Tool struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Request 是解释器的输入。Options 是 Agent DSL 里透传给渠道的私有参数，
// Cred 只含**非密**凭据字段（如 region）——secret 字段永远不进这里，它只
// 能被 auth 驱动使用，这样描述符没有任何办法把 API Key 拼进 body 或 URL。
type Request struct {
	Model       string            `json:"model,omitempty"`
	Messages    []Message         `json:"messages,omitempty"`
	Tools       []Tool            `json:"tools,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
	Options     map[string]any    `json:"options,omitempty"`
	Cred        map[string]string `json:"cred,omitempty"`
}

// Result 是一次调用归一后的结果，流式和非流式两条路径必须产出同构的
// Result——fixtures 自检就是拿同一份期望值同时校验两边的。
type Result struct {
	Content      string     `json:"content,omitempty"`
	Reasoning    string     `json:"reasoning,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	InputTokens  int64      `json:"input_tokens,omitempty"`
	OutputTokens int64      `json:"output_tokens,omitempty"`
}

// Delta 是流式过程中的一个增量片段。工具调用的参数是一片片攒的 JSON，
// 半截 JSON 推给前端既显示不了也执行不了，所以不在 Delta 里出现——它只在
// 块结束、解析成功后才作为 Result.ToolCalls 出现。
type Delta struct {
	Text      string `json:"text,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// HTTPRequest 是描述符渲染出的请求描述。宿主拿它拼真实的 *http.Request，
// 并在那一步（且只在那一步）注入凭据。
type HTTPRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Query   map[string]string
	Body    any
}
