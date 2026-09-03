package descriptor

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Version 是描述符规范版本。改动不兼容时递增，老描述符加载期报错而不是
// 运行期给出诡异结果。
const Version = 1

// Descriptor 是一个模型渠道的完整声明。
type Descriptor struct {
	DescriptorVersion int    `json:"descriptor_version"`
	ID                string `json:"id"`
	Label             string `json:"label"`
	Wire              string `json:"wire"`
	// Description 是给管理员看的说明（协议模板选择器里显示），不参与任何
	// 运行时行为。
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	BaseURL      string   `json:"base_url"`

	Credentials []CredentialField     `json:"credentials"`
	Auth        Auth                  `json:"auth"`
	Messages    MessagesConfig        `json:"messages"`
	Complete    *Operation            `json:"complete"`
	Stream      *StreamConfig         `json:"stream"`
	Embed       *EmbedConfig          `json:"embed"`
	Probe       *Probe                `json:"probe"`
	Pricing     map[string]ModelPrice `json:"pricing"`
}

// CredentialField 驱动前端表单和 provider_keys 的存储形状。type 为
// "secret" 的字段对表达式不可见（见 Request.Cred 的注释）。
type CredentialField struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // secret | text | url
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// Auth 只能**点名**宿主实现的驱动并指定用哪个凭据字段，不能实现算法。
// 签名类（sigv4 等）需要读 body 做摘要，本质上不可声明式表达——这是有意
// 封口，新增一种签名 = 改 Go = 发版本，可以接受，因为签名协议的新增频率
// 远低于新增渠道。
type Auth struct {
	Driver     string `json:"driver"` // none | bearer | header | query
	Credential string `json:"credential"`
	Header     string `json:"header"`
	Prefix     string `json:"prefix"`
	Query      string `json:"query"`
}

// MessagesConfig 描述统一消息怎么铺成厂商的消息形状。这是厂商差异最集中
// 的地方，做成结构化配置而不是让每个描述符自己写 $map——否则同样的翻译
// 逻辑会在每份描述符里各写一遍，各错一遍。
type MessagesConfig struct {
	// System: "hoist" 把 system 从消息列表里抽出来，作为 $.system 暴露给
	// body 模板（放到哪个字段由描述符自己决定）；"inline" 保留为首条消息。
	System string `json:"system"`
	// Roles 把统一角色映射成厂商角色，缺省时原样使用。
	Roles map[string]string `json:"roles"`
	// Content: "string" 单条消息的 content 是纯文本；"parts" 是分块数组。
	Content string `json:"content"`

	TextPart       map[string]any `json:"text_part"`
	ToolCallPart   map[string]any `json:"tool_call_part"`
	ToolResultPart map[string]any `json:"tool_result_part"`

	// ToolCalls: "sibling_field" 工具调用挂在消息的同级字段（OpenAI 的
	// tool_calls）；"inline_parts" 混在 content 分块里（Anthropic）。
	ToolCalls      string `json:"tool_calls"`
	ToolCallsField string `json:"tool_calls_field"`
	// ToolResultRole 是工具结果消息用哪个角色发。Anthropic 把它塞进 user
	// 消息，OpenAI 用独立的 tool 角色。
	ToolResultRole string `json:"tool_result_role"`
	// ToolsWrapper 是工具**定义**铺成什么形状：
	//   "openai"（默认）{"type":"function","function":{name,description,parameters}}
	//   "flat"          {name,description,input_schema}（Anthropic Messages）
	//   "openai_responses" {"type":"function",name,description,parameters}
	//                   （Responses API 把 function 那一层摊平了）
	// 各家的差异只有这一层包装，所以用一个枚举而不是再给一份模板。
	ToolsWrapper string `json:"tools_wrapper"`
}

// Operation 是一次 HTTP 调用的模板 + 响应映射。
type Operation struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Headers  map[string]string `json:"headers"`
	Query    map[string]string `json:"query"`
	Body     map[string]any    `json:"body"`
	Response ResponseMap       `json:"response"`
}

// ResponseMap 是非流式响应的路径映射。text/reasoning 是候选列表，依次尝
// 试第一个取到值的——同一家厂商不同模型放的位置可能不同。
type ResponseMap struct {
	Text      []string      `json:"text"`
	Reasoning []string      `json:"reasoning"`
	ToolCalls *ToolCallsMap `json:"tool_calls"`
	Usage     *UsageMap     `json:"usage"`
	Error     *ErrorMap     `json:"error"`
}

// ToolCallsMap 里 ArgumentsJSON 和 Arguments 是二选一：前者声明"上游给的
// 是一段 JSON 字符串，需要解析"（OpenAI），后者声明"上游直接给对象"
// （Anthropic）。用不同键名区分，而不是让解释器猜。
type ToolCallsMap struct {
	Each          string `json:"each"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	ArgumentsJSON string `json:"arguments_json"`
	Arguments     string `json:"arguments"`
}

type UsageMap struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// ErrorMap 的 When 是"这个路径有值就说明上游报错了"。HTTP 状态码由宿主判
// 断，这里管的是 200 里夹着业务错误的情况。
type ErrorMap struct {
	When    string `json:"when"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// StreamConfig 是流式段。Request.Patch 与 complete.body 浅合并——流式和非
// 流式共享同一个请求模板，只声明差异，这条约束顺带保证两条路径不会漂移。
type StreamConfig struct {
	Request struct {
		Patch map[string]any `json:"patch"`
	} `json:"request"`
	Transport string   `json:"transport"` // sse | ndjson
	Done      []string `json:"done"`
	// EventType 取事件类型的位置。"$event" 表示取 SSE 的 event: 行；其它
	// 值当作事件负载内的路径（有些厂商把类型写在 data 里）。留空表示这个
	// 渠道的流没有事件类型，所有规则都对每个事件尝试。
	EventType string `json:"event_type"`
	On        []Rule `json:"on"`
}

// Rule 是状态机的一条规则。规则**按声明顺序全部尝试**，不是首个命中即
// 停——一个 SSE 事件里可能同时带 delta 和 usage。
//
// 可用操作是封闭集：没有循环、没有赋值、没有用户自定义变量，所以整台状
// 态机可枚举、可测、可 fuzz，绝不图灵完备。
type Rule struct {
	// Match 匹配事件类型；空或 "*" 表示不限。
	Match string `json:"match"`
	// Switch/Cases 在事件内部再分派一层（Anthropic 的 delta.type）。
	Switch string          `json:"switch"`
	Cases  map[string]*Ops `json:"cases"`

	Ops
}

// Ops 是一组操作。Each 非空时，Index/SetID/SetName/AppendArgs 在数组元素
// 的作用域里求值（OpenAI 的 delta.tool_calls[]）；否则在事件负载上求值。
type Ops struct {
	EmitText      string  `json:"emit_text"`
	EmitReasoning string  `json:"emit_reasoning"`
	Each          string  `json:"each"`
	Index         string  `json:"index"`
	Open          *OpenOp `json:"open"`
	SetID         string  `json:"set_id"`
	SetName       string  `json:"set_name"`
	AppendArgs    string  `json:"append_args"`
	Close         string  `json:"close"`
	UsageIn       string  `json:"usage_in"`
	UsageOut      string  `json:"usage_out"`
	Fail          string  `json:"fail"`
}

// OpenOp 显式开一个索引槽。没有 open/close 的渠道（OpenAI）槽位按 index
// 隐式创建、流结束时隐式关闭。
type OpenOp struct {
	Index   string `json:"index"`
	Kind    string `json:"kind"`
	SetID   string `json:"set_id"`
	SetName string `json:"set_name"`
}

// EmbedConfig 是向量化。它不参与降级链（不同模型的向量不可互换），所以
// 结构比对话简单得多。
type EmbedConfig struct {
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	Headers  map[string]string `json:"headers"`
	Body     map[string]any    `json:"body"`
	Response struct {
		Each   string `json:"each"`
		Vector string `json:"vector"`
	} `json:"response"`
}

// Probe 是登记凭据时的连通性校验：只验鉴权，不花 token。
type Probe struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// ModelPrice 是每 1000 token 的美元单价。渠道自己带价格表，比在 Go 里维
// 护第二张表好——加渠道就不用同时改两个地方。
type ModelPrice struct {
	InputPer1K  float64 `json:"input_per_1k"`
	OutputPer1K float64 `json:"output_per_1k"`
}

var (
	knownCapabilities = map[string]bool{"text": true, "tools": true, "stream": true, "embed": true}
	knownAuthDrivers  = map[string]bool{"": true, "none": true, "bearer": true, "header": true, "query": true}
	knownTransports   = map[string]bool{"sse": true, "ndjson": true}
)

// Load 解析并**完整校验**一份描述符。校验一次性收集全部错误再返回，而不
// 是遇到第一个就停——装一个渠道时把问题一次看完，比改一个装一次快得多。
func Load(data []byte) (*Descriptor, error) {
	var d Descriptor
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("descriptor: 解析失败: %w", err)
	}
	if errs := d.validate(); len(errs) > 0 {
		return nil, fmt.Errorf("descriptor %q 校验失败:\n  - %s", d.ID, strings.Join(errs, "\n  - "))
	}
	d.applyDefaults()
	return &d, nil
}

func (d *Descriptor) applyDefaults() {
	if d.Messages.Content == "" {
		d.Messages.Content = "string"
	}
	if d.Messages.System == "" {
		d.Messages.System = "inline"
	}
	if d.Messages.ToolCalls == "" {
		d.Messages.ToolCalls = "sibling_field"
	}
	if d.Messages.ToolCallsField == "" {
		d.Messages.ToolCallsField = "tool_calls"
	}
	if d.Stream != nil && d.Stream.Transport == "" {
		d.Stream.Transport = "sse"
	}
}

func (d *Descriptor) Has(capability string) bool {
	for _, c := range d.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

func (d *Descriptor) validate() []string {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if d.DescriptorVersion != Version {
		add("descriptor_version 必须是 %d，得到 %d", Version, d.DescriptorVersion)
	}
	if d.ID == "" {
		add("缺少 id")
	}
	if d.Label == "" {
		add("缺少 label")
	}
	for _, c := range d.Capabilities {
		if !knownCapabilities[c] {
			add("未知能力 %q", c)
		}
	}
	if !knownAuthDrivers[d.Auth.Driver] {
		add("未知鉴权驱动 %q（宿主实现的驱动是封闭集）", d.Auth.Driver)
	}
	if d.Auth.Driver == "header" && d.Auth.Header == "" {
		add("auth.driver=header 必须指定 auth.header")
	}
	if d.Auth.Driver == "query" && d.Auth.Query == "" {
		add("auth.driver=query 必须指定 auth.query")
	}
	// 凭据引用必须落在真实声明的字段上，否则运行时会拿到空 key 去请求，
	// 然后收获一个含义模糊的 401。
	if d.Auth.Credential != "" && !d.hasCredential(d.Auth.Credential) {
		add("auth.credential 指向未声明的凭据字段 %q", d.Auth.Credential)
	}
	for _, f := range d.Credentials {
		if f.Name == "" {
			add("credentials[] 里有一项缺少 name")
		}
		if f.Type != "secret" && f.Type != "text" && f.Type != "url" {
			add("凭据字段 %q 的 type 必须是 secret/text/url", f.Name)
		}
	}

	// capabilities 是声明，和实际存在的段交叉校验。声明了 stream 却没有
	// stream 段，等到运行时才发现"这渠道其实不支持流"就太晚了。
	if d.Has("text") && d.Complete == nil {
		add("声明了 text 能力但缺少 complete 段")
	}
	if d.Has("stream") && d.Stream == nil {
		add("声明了 stream 能力但缺少 stream 段")
	}
	if d.Stream != nil && !d.Has("stream") {
		add("有 stream 段但 capabilities 里没有声明 stream")
	}
	if d.Has("embed") && d.Embed == nil {
		add("声明了 embed 能力但缺少 embed 段")
	}

	if d.Complete != nil {
		errs = append(errs, validateOperation("complete", *d.Complete)...)
	}
	if d.Stream != nil {
		errs = append(errs, d.validateStream()...)
	}
	if d.Embed != nil {
		if d.Embed.Path == "" {
			add("embed.path 不能为空")
		}
		validateTemplate(toAny(d.Embed.Body), "embed.body", nil, &errs)
	}
	errs = append(errs, d.validateMessages()...)

	// $.cred.<x> 只能取非密字段。这条杜绝了"某个渠道把 API Key 拼进 body
	// 或 URL"这条泄漏路径——凭据只能经宿主的鉴权驱动进入请求。
	errs = append(errs, d.validateCredRefs()...)
	return errs
}

func (d *Descriptor) hasCredential(name string) bool {
	for _, f := range d.Credentials {
		if f.Name == name {
			return true
		}
	}
	return false
}

func validateOperation(label string, op Operation) []string {
	var errs []string
	if op.Method == "" {
		errs = append(errs, label+".method 不能为空")
	}
	if op.Path == "" {
		errs = append(errs, label+".path 不能为空")
	}
	validateTemplate(toAny(op.Body), label+".body", nil, &errs)
	for k, v := range op.Headers {
		validateTemplate(v, label+".headers."+k, nil, &errs)
	}
	for k, v := range op.Query {
		validateTemplate(v, label+".query."+k, nil, &errs)
	}
	if op.Response.ToolCalls != nil {
		tc := op.Response.ToolCalls
		if tc.Each == "" {
			errs = append(errs, label+".response.tool_calls.each 不能为空")
		}
		if tc.ArgumentsJSON == "" && tc.Arguments == "" {
			errs = append(errs, label+".response.tool_calls 必须指定 arguments_json 或 arguments")
		}
		if tc.ArgumentsJSON != "" && tc.Arguments != "" {
			errs = append(errs, label+".response.tool_calls 的 arguments_json 和 arguments 只能二选一")
		}
	}
	return errs
}

func (d *Descriptor) validateStream() []string {
	var errs []string
	s := d.Stream
	if s.Transport != "" && !knownTransports[s.Transport] {
		errs = append(errs, fmt.Sprintf("stream.transport 只支持 sse/ndjson，得到 %q", s.Transport))
	}
	if len(s.On) == 0 {
		errs = append(errs, "stream.on 至少要有一条规则")
	}
	validateTemplate(toAny(s.Request.Patch), "stream.request.patch", nil, &errs)
	for i, rule := range s.On {
		label := fmt.Sprintf("stream.on[%d]", i)
		if rule.Switch != "" && len(rule.Cases) == 0 {
			errs = append(errs, label+" 有 switch 但没有 cases")
		}
		if rule.Switch == "" && len(rule.Cases) > 0 {
			errs = append(errs, label+" 有 cases 但没有 switch")
		}
		errs = append(errs, validateOps(label, rule.Ops, rule.Index != "")...)
		for name, ops := range rule.Cases {
			if ops == nil {
				errs = append(errs, fmt.Sprintf("%s.cases.%s 是空的", label, name))
				continue
			}
			errs = append(errs, validateOps(fmt.Sprintf("%s.cases.%s", label, name), *ops, rule.Index != "")...)
		}
	}
	return errs
}

// validateOps 盯住一条：写槽位的操作必须知道写哪个槽。要么规则自带
// index，要么 each 的元素里带 index，两者都没有就是个必然在运行时静默丢
// 数据的错误。
func validateOps(label string, ops Ops, inheritsIndex bool) []string {
	var errs []string
	slotted := ops.SetID != "" || ops.SetName != "" || ops.AppendArgs != "" || ops.Open != nil
	hasIndex := inheritsIndex || ops.Index != "" || (ops.Open != nil && ops.Open.Index != "")
	if slotted && !hasIndex {
		errs = append(errs, label+" 写了槽位（set_id/set_name/append_args/open）但没有 index，无法确定写哪个槽")
	}
	if ops.Each != "" && ops.Index == "" && !inheritsIndex {
		errs = append(errs, label+" 有 each 但没有 index，无法把每个元素对上槽位")
	}
	return errs
}

func (d *Descriptor) validateMessages() []string {
	var errs []string
	m := d.Messages
	switch m.ToolsWrapper {
	case "", "openai", "flat", "openai_responses":
	default:
		errs = append(errs, fmt.Sprintf("messages.tools_wrapper 只支持 openai/flat/openai_responses，得到 %q", m.ToolsWrapper))
	}
	if m.System != "" && m.System != "hoist" && m.System != "inline" {
		errs = append(errs, fmt.Sprintf("messages.system 只支持 hoist/inline，得到 %q", m.System))
	}
	if m.Content != "" && m.Content != "string" && m.Content != "parts" {
		errs = append(errs, fmt.Sprintf("messages.content 只支持 string/parts，得到 %q", m.Content))
	}
	if m.ToolCalls != "" && m.ToolCalls != "sibling_field" && m.ToolCalls != "inline_parts" {
		errs = append(errs, fmt.Sprintf("messages.tool_calls 只支持 sibling_field/inline_parts，得到 %q", m.ToolCalls))
	}
	if m.Content == "parts" && len(m.TextPart) == 0 {
		errs = append(errs, "messages.content=parts 必须提供 text_part 模板")
	}
	// 消息模板在元素作用域里求值，变量是这几个固定名字。
	partVars := map[string]bool{"text": true, "id": true, "name": true, "arguments": true, "content": true, "role": true}
	validateTemplate(toAny(m.TextPart), "messages.text_part", partVars, &errs)
	validateTemplate(toAny(m.ToolCallPart), "messages.tool_call_part", partVars, &errs)
	validateTemplate(toAny(m.ToolResultPart), "messages.tool_result_part", partVars, &errs)
	return errs
}

func (d *Descriptor) validateCredRefs() []string {
	secrets := map[string]bool{}
	for _, f := range d.Credentials {
		if f.Type == "secret" {
			secrets[f.Name] = true
		}
	}
	if len(secrets) == 0 {
		return nil
	}
	var errs []string
	var walkTemplate func(node any, path string)
	walkTemplate = func(node any, path string) {
		switch n := node.(type) {
		case string:
			if ref, ok := strings.CutPrefix(n, "$.cred."); ok {
				field := ref
				if i := strings.IndexByte(field, '.'); i >= 0 {
					field = field[:i]
				}
				if secrets[field] {
					errs = append(errs, fmt.Sprintf(
						"%s: 不能引用 secret 字段 $.cred.%s——凭据只能经 auth 驱动进入请求，"+
							"表达式里取不到它（防止把密钥拼进 body 或 URL）", path, field))
				}
			}
		case []any:
			for i, item := range n {
				walkTemplate(item, fmt.Sprintf("%s[%d]", path, i))
			}
		case map[string]any:
			for _, k := range sortedKeys(n) {
				walkTemplate(n[k], path+"."+k)
			}
		}
	}
	if d.Complete != nil {
		walkTemplate(toAny(d.Complete.Body), "complete.body")
		for k, v := range d.Complete.Headers {
			walkTemplate(v, "complete.headers."+k)
		}
		for k, v := range d.Complete.Query {
			walkTemplate(v, "complete.query."+k)
		}
	}
	if d.Stream != nil {
		walkTemplate(toAny(d.Stream.Request.Patch), "stream.request.patch")
	}
	if d.Embed != nil {
		walkTemplate(toAny(d.Embed.Body), "embed.body")
	}
	return errs
}

func toAny(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

// IsSecret 说一个凭据字段是不是密文。descriptor_client 用它把 secret 从
// 表达式作用域里剔掉。
func (d *Descriptor) IsSecret(name string) bool {
	for _, f := range d.Credentials {
		if f.Name == name {
			return f.Type == "secret"
		}
	}
	// 没声明的字段一律当密文对待：漏声明的后果是"取不到"，而不是"密钥
	// 泄漏"。
	return true
}
