package descriptor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// 流式的核心认知：**流不是"响应"，是一串改写累加器的事件。**
//
// 所以正确的抽象不是路径映射（那只能描述"一坨完整 JSON 里内容在哪"），而
// 是「固定的累加器槽 + 一组封闭操作的事件规则」。Anthropic 那种
// content_block_start 开槽、input_json_delta 一片片攒 JSON、
// content_block_stop 才能解析的流，用路径映射根本表达不了——同类方案
// （open-ai-canvas 的 yingce.plugin/v2）正是因为这个直接放弃了流式。
//
// 累加器是固定且有限的（下面的 accumulator），可用操作也是封闭集，没有循
// 环、没有赋值、没有用户自定义变量，所以整台状态机可枚举、可测、可 fuzz。

// block 是一个索引槽。text 类块的文字在 emit_text 时就直接进了全局
// text，这里只留 args 给工具调用攒参数。
type block struct {
	kind string
	id   string
	name string
	args strings.Builder
}

type accumulator struct {
	text      strings.Builder
	reasoning strings.Builder
	blocks    map[string]*block
	order     []string
	inTokens  int64
	outTokens int64
	failure   error
}

func newAccumulator() *accumulator {
	return &accumulator{blocks: map[string]*block{}}
}

// slot 取（或隐式创建）一个索引槽。没有 open/close 的渠道（OpenAI 的
// delta.tool_calls[]）就靠这条隐式创建，流结束时统一收尾。
func (a *accumulator) slot(key string) *block {
	if b, ok := a.blocks[key]; ok {
		return b
	}
	b := &block{}
	a.blocks[key] = b
	a.order = append(a.order, key)
	return b
}

// result 收口：把累加器变成统一结果。
//
// 一个槽变成工具调用的判据是**它有名字**——Anthropic 的 text 块也是槽，
// 但它没有 name，文字早在 emit_text 时进了全局 text。
func (a *accumulator) result() (Result, error) {
	res := Result{
		Content:      a.text.String(),
		Reasoning:    a.reasoning.String(),
		InputTokens:  a.inTokens,
		OutputTokens: a.outTokens,
	}
	for _, key := range a.order {
		b := a.blocks[key]
		if b.name == "" {
			continue
		}
		args, err := decodeArgs(b.args.String())
		if err != nil {
			// 静默丢一个工具调用，agent 会卡在"我以为我调了"的状态上，
			// 比直接报错难查十倍。所以这里让整轮失败。
			return Result{}, fmt.Errorf("工具调用 %q 的流式参数拼出来不是合法 JSON: %w", b.name, err)
		}
		res.ToolCalls = append(res.ToolCalls, ToolCall{ID: b.id, Name: b.name, Arguments: args})
	}
	return res, nil
}

// RunStream 消费一条流，按状态机规则累加，最终产出与非流式同构的 Result。
// onDelta 可以为 nil（比如 fixtures 自检里只关心最终结果）。
func (d *Descriptor) RunStream(body io.Reader, onDelta func(Delta)) (Result, error) {
	if d.Stream == nil {
		return Result{}, fmt.Errorf("descriptor %s: 没有 stream 段", d.ID)
	}
	acc := newAccumulator()
	emit := func(delta Delta) {
		if onDelta != nil {
			onDelta(delta)
		}
	}

	err := d.scanEvents(body, func(eventType string, data []byte) bool {
		if d.isDone(data) {
			return false
		}
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			// 心跳、注释、非 JSON 的保活帧：跳过而不是让整轮失败。
			return true
		}
		for i := range d.Stream.On {
			d.applyRule(&d.Stream.On[i], eventType, payload, acc, emit)
			if acc.failure != nil {
				return false
			}
		}
		return true
	})
	if err != nil {
		return Result{}, err
	}
	if acc.failure != nil {
		return Result{}, acc.failure
	}
	return acc.result()
}

func (d *Descriptor) isDone(data []byte) bool {
	trimmed := strings.TrimSpace(string(data))
	for _, sentinel := range d.Stream.Done {
		if trimmed == sentinel {
			return true
		}
	}
	return false
}

// applyRule 按固定顺序施加一条规则的操作。规则按声明顺序**全部尝试**，
// 不是首个命中即停——一个 SSE 事件里可能同时带 delta 和 usage。
func (d *Descriptor) applyRule(rule *Rule, eventType string, payload any, acc *accumulator, emit func(Delta)) {
	if !matches(rule.Match, eventType) {
		return
	}
	// switch/cases 在事件内部再分派一层（Anthropic 的 delta.type）。
	// 规则级的 index 会被 case 继承——不然每个 case 都要重复写一遍。
	if rule.Switch != "" {
		key := toString(lookup(payload, rule.Switch))
		ops, ok := rule.Cases[key]
		if !ok || ops == nil {
			return
		}
		merged := *ops
		if merged.Index == "" {
			merged.Index = rule.Index
		}
		applyOps(merged, payload, acc, emit)
		return
	}
	applyOps(rule.Ops, payload, acc, emit)
}

func matches(match, eventType string) bool {
	return match == "" || match == "*" || match == eventType
}

func applyOps(ops Ops, payload any, acc *accumulator, emit func(Delta)) {
	// each：在数组元素的作用域里施加槽位操作（OpenAI 的 delta.tool_calls[]）。
	if ops.Each != "" {
		rows, _ := lookup(payload, ops.Each).([]any)
		for _, row := range rows {
			applySlotOps(ops, row, acc)
		}
	} else {
		applySlotOps(ops, payload, acc)
	}

	// emit_text 同时做两件事：推给调用方**并且**追加到最终文本。合成一
	// 个操作是有意的——分成两个，渠道作者迟早漏掉其中一件。
	if text := toString(lookup(payload, ops.EmitText)); ops.EmitText != "" && text != "" {
		acc.text.WriteString(text)
		emit(Delta{Text: text})
	}
	if r := toString(lookup(payload, ops.EmitReasoning)); ops.EmitReasoning != "" && r != "" {
		acc.reasoning.WriteString(r)
		emit(Delta{Reasoning: r})
	}

	if ops.Close != "" {
		// 显式 close 目前不需要额外动作（参数在 result() 里统一解析），
		// 但保留这个操作：它让描述符读起来和上游协议一一对应，也给以后
		// "块结束才 emit"的渠道留了钩子。
		_ = lookup(payload, ops.Close)
	}

	// usage 常常只在最后一个事件里出现，且有的厂商每个事件都带一份零
	// 值——所以只在取到正数时才覆盖。
	if ops.UsageIn != "" {
		if n := toInt64(lookup(payload, ops.UsageIn)); n > 0 {
			acc.inTokens = n
		}
	}
	if ops.UsageOut != "" {
		if n := toInt64(lookup(payload, ops.UsageOut)); n > 0 {
			acc.outTokens = n
		}
	}
	if ops.Fail != "" {
		if msg := toString(lookup(payload, ops.Fail)); msg != "" {
			acc.failure = errors.New(msg)
		}
	}
}

// applySlotOps 施加需要知道"写哪个槽"的那几个操作。
func applySlotOps(ops Ops, payload any, acc *accumulator) {
	if ops.Open != nil {
		key := toString(lookup(payload, ops.Open.Index))
		b := acc.slot(key)
		setIfPresent(&b.kind, lookup(payload, ops.Open.Kind))
		setIfPresent(&b.id, lookup(payload, ops.Open.SetID))
		setIfPresent(&b.name, lookup(payload, ops.Open.SetName))
	}
	if ops.Index == "" {
		return
	}
	if ops.SetID == "" && ops.SetName == "" && ops.AppendArgs == "" {
		return
	}
	key := toString(lookup(payload, ops.Index))
	b := acc.slot(key)
	// 只在有值时覆盖：OpenAI 的工具调用只有第一个分片带 id 和 name，后
	// 续分片只有 arguments，照抄会把 name 冲成空。
	setIfPresent(&b.id, lookup(payload, ops.SetID))
	setIfPresent(&b.name, lookup(payload, ops.SetName))
	if ops.AppendArgs != "" {
		// append_args 只累加、不 emit——半截 JSON 推给前端既显示不了也
		// 执行不了。
		b.args.WriteString(toString(lookup(payload, ops.AppendArgs)))
	}
}

func setIfPresent(dst *string, v any) {
	if s := toString(v); s != "" {
		*dst = s
	}
}

const maxStreamLine = 1 << 20

// scanEvents 分帧。onEvent 返回 false 表示提前收流（命中 done sentinel 或
// fail）。分帧完全由宿主实现，描述符不碰——它只声明 transport。
func (d *Descriptor) scanEvents(body io.Reader, onEvent func(eventType string, data []byte) bool) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	if d.Stream.Transport == "ndjson" {
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			if !onEvent("", append([]byte(nil), line...)) {
				return nil
			}
		}
		return scanner.Err()
	}

	// SSE：event: 行给出事件类型，data: 行累积成一帧，空行结束一帧。
	var eventType string
	var data bytes.Buffer
	flush := func() bool {
		defer func() {
			eventType = ""
			data.Reset()
		}()
		if data.Len() == 0 {
			return true
		}
		return onEvent(d.eventTypeOf(eventType, data.Bytes()), append([]byte(nil), data.Bytes()...))
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		switch {
		case line == "":
			if !flush() {
				return nil
			}
		case strings.HasPrefix(line, ":"):
			// 注释 / 保活，忽略。
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	flush()
	return nil
}

// eventTypeOf 决定一帧的事件类型：默认取 SSE 的 event: 行（"$event"），
// 也支持从负载内部取——有的厂商不发 event: 行，把类型写在 data 里。
func (d *Descriptor) eventTypeOf(sseEvent string, data []byte) string {
	path := d.Stream.EventType
	if path == "" || path == "$event" {
		return sseEvent
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return sseEvent
	}
	return toString(lookup(payload, path))
}
