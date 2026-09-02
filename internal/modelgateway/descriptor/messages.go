package descriptor

import "fmt"

// renderMessages 把统一消息铺成厂商的消息形状，并在 system=hoist 时把系
// 统提示词单独抽出来（放到请求体的哪里由 body 模板自己用 $.system 决定，
// 解释器不替描述符做这个决定）。
//
// 这一段是整个描述符里最"结构化"的部分，刻意没做成让每份描述符自己写
// $map——OpenAI 和 Anthropic 的消息差异如果各写各的 $map，同样的翻译逻辑
// 会在每份描述符里各错一遍。
func (d *Descriptor) renderMessages(msgs []Message) (rendered []any, system string, err error) {
	cfg := d.Messages
	out := make([]any, 0, len(msgs))

	for _, msg := range msgs {
		if msg.Role == "system" && cfg.System == "hoist" {
			if system != "" {
				system += "\n\n"
			}
			system += msg.Content
			continue
		}

		if msg.Role == "tool" {
			item, err := d.renderToolResult(msg)
			if err != nil {
				return nil, "", err
			}
			if item != nil {
				out = append(out, item)
			}
			continue
		}

		item, err := d.renderTurn(msg)
		if err != nil {
			return nil, "", err
		}
		out = append(out, item)
	}
	return out, system, nil
}

func (d *Descriptor) mapRole(role string) string {
	if mapped, ok := d.Messages.Roles[role]; ok && mapped != "" {
		return mapped
	}
	return role
}

func (d *Descriptor) renderTurn(msg Message) (map[string]any, error) {
	cfg := d.Messages
	item := map[string]any{"role": d.mapRole(msg.Role)}

	switch cfg.Content {
	case "parts":
		parts := make([]any, 0, 1+len(msg.ToolCalls))
		if msg.Content != "" {
			part, err := render(cfg.TextPart, map[string]any{"text": msg.Content})
			if err != nil {
				return nil, fmt.Errorf("text_part: %w", err)
			}
			parts = append(parts, part)
		}
		if cfg.ToolCalls == "inline_parts" {
			for _, tc := range msg.ToolCalls {
				part, err := render(cfg.ToolCallPart, toolCallScope(tc))
				if err != nil {
					return nil, fmt.Errorf("tool_call_part: %w", err)
				}
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			item["content"] = parts
		}
	default:
		// content=string 时空字符串也要保留：一条只带 tool_calls 的
		// assistant 消息，有些厂商要求 content 字段在场（可以为空）。
		item["content"] = msg.Content
	}

	if cfg.ToolCalls == "sibling_field" && len(msg.ToolCalls) > 0 {
		calls := make([]any, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			part, err := render(cfg.ToolCallPart, toolCallScope(tc))
			if err != nil {
				return nil, fmt.Errorf("tool_call_part: %w", err)
			}
			calls = append(calls, part)
		}
		item[cfg.ToolCallsField] = calls
	}
	return item, nil
}

func (d *Descriptor) renderToolResult(msg Message) (any, error) {
	cfg := d.Messages
	vars := map[string]any{
		"id":      msg.ToolCallID,
		"name":    msg.ToolName,
		"content": msg.Content,
		"text":    msg.Content,
	}
	if len(cfg.ToolResultPart) == 0 {
		// 没声明模板 = 这个渠道的工具结果就是一条普通消息。
		return map[string]any{
			"role":         d.mapRole("tool"),
			"content":      msg.Content,
			"tool_call_id": msg.ToolCallID,
		}, nil
	}
	part, err := render(cfg.ToolResultPart, vars)
	if err != nil {
		return nil, fmt.Errorf("tool_result_part: %w", err)
	}
	if cfg.Content == "parts" {
		role := cfg.ToolResultRole
		if role == "" {
			role = d.mapRole("tool")
		}
		return map[string]any{"role": role, "content": []any{part}}, nil
	}
	return part, nil
}

func toolCallScope(tc ToolCall) map[string]any {
	args := tc.Arguments
	if args == nil {
		args = map[string]any{}
	}
	return map[string]any{
		"id":        tc.ID,
		"name":      tc.Name,
		"arguments": mapToAny(args),
	}
}

func mapToAny(m map[string]any) any {
	if len(m) == 0 {
		// 空参数要发成 {} 而不是被 omitempty 吃掉——多数厂商对
		// arguments 缺失的容忍度远低于对空对象。
		return keepMarker{map[string]any{}}
	}
	return m
}

// render 求值一个元素级模板（消息分块那种），变量表就是传进来的这几个。
func render(tmpl map[string]any, vars map[string]any) (any, error) {
	if len(tmpl) == 0 {
		return nil, nil
	}
	v, err := eval(map[string]any(tmpl), newScope(vars))
	if err != nil {
		return nil, err
	}
	return unwrap(v), nil
}

// renderTools 把工具定义铺成厂商形状。它没有单独的模板段——所有主流厂商
// 的工具定义都是 {name, description, parameters} 的一层包装差异，用
// tools_wrapper 一个字段就够，多给一个模板反而是负担。
func (d *Descriptor) renderTools(tools []Tool) []any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	return out
}
