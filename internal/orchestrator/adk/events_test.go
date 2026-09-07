package adk

import (
	"testing"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestTranslateEvent_FunctionCall_IsToolCallStarted(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromParts([]*genai.Part{
		{FunctionCall: &genai.FunctionCall{Name: "search", Args: map[string]any{"q": "go"}}},
	}, genai.RoleModel)

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeToolCallStarted {
		t.Fatalf("unexpected translation: %+v", events)
	}
	if events[0].Payload["name"] != "search" {
		t.Fatalf("expected tool name in payload, got %+v", events[0].Payload)
	}
}

func TestTranslateEvent_FunctionResponse_IsToolCallFinished(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromParts([]*genai.Part{
		{FunctionResponse: &genai.FunctionResponse{Name: "search", Response: map[string]any{"result": "ok"}}},
	}, genai.RoleModel)

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeToolCallFinished {
		t.Fatalf("unexpected translation: %+v", events)
	}
}

func TestTranslateEvent_Thought_BecomesReasoning(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromParts([]*genai.Part{
		{Text: "let me think about this...", Thought: true},
	}, genai.RoleModel)

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeReasoning {
		t.Fatalf("expected a node.reasoning event, got %+v", events)
	}
}

func TestTranslateEvent_FinalText_IsNodeFinished(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromText("the final answer", genai.RoleModel)
	// No function calls/responses, not Partial: IsFinalResponse() is true.

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeFinished {
		t.Fatalf("expected a node.finished event, got %+v", events)
	}
	if events[0].Payload["text"] != "the final answer" {
		t.Fatalf("unexpected payload: %+v", events[0].Payload)
	}
}

func TestTranslateEvent_PartialText_IsPublicThinking(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromText("partial chunk", genai.RoleModel)
	ev.Partial = true // streaming, not yet the committed final response

	events := TranslateEvent("architect", ev)
	// 流式增量文本对所有身份可见。它是订阅者过一会儿照样会整段收到的那个
	// 答案的前缀——把它挡掉，聊天页就只能一次性蹦出整段，没有打字机效果。
	if len(events) != 1 || events[0].Type != EventNodeThinking {
		t.Fatalf("expected a node.thinking event for partial text, got %+v", events)
	}
}

func TestTranslateEvent_Nil_ReturnsNothing(t *testing.T) {
	if got := TranslateEvent("architect", nil); got != nil {
		t.Fatalf("expected nil for a nil event, got %+v", got)
	}
}

func TestTranslateEvent_CarriesUsageAndCost(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromText("final answer", genai.RoleModel)
	ev.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 4}
	ev.CustomMetadata = map[string]any{"cost_usd": 0.0123}

	events := TranslateEvent("architect", ev)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %+v", events)
	}
	if events[0].InputTokens != 10 || events[0].OutputTokens != 4 || events[0].CostUSD != 0.0123 {
		t.Fatalf("unexpected usage on translated event: %+v", events[0])
	}
}
