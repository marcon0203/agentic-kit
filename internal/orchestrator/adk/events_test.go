package adk

import (
	"testing"

	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestTranslateEvent_FunctionCall_IsToolCallStartedNotInternal(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromParts([]*genai.Part{
		{FunctionCall: &genai.FunctionCall{Name: "search", Args: map[string]any{"q": "go"}}},
	}, genai.RoleModel)

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeToolCallStarted || events[0].IsInternal {
		t.Fatalf("unexpected translation: %+v", events)
	}
	if events[0].Payload["name"] != "search" {
		t.Fatalf("expected tool name in payload, got %+v", events[0].Payload)
	}
}

func TestTranslateEvent_FunctionResponse_IsToolCallFinishedNotInternal(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromParts([]*genai.Part{
		{FunctionResponse: &genai.FunctionResponse{Name: "search", Response: map[string]any{"result": "ok"}}},
	}, genai.RoleModel)

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeToolCallFinished || events[0].IsInternal {
		t.Fatalf("unexpected translation: %+v", events)
	}
}

func TestTranslateEvent_Thought_IsInternal(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromParts([]*genai.Part{
		{Text: "let me think about this...", Thought: true},
	}, genai.RoleModel)

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeThinking || !events[0].IsInternal {
		t.Fatalf("expected an internal node.thinking event, got %+v", events)
	}
}

func TestTranslateEvent_FinalText_IsNodeFinishedNotInternal(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromText("the final answer", genai.RoleModel)
	// No function calls/responses, not Partial: IsFinalResponse() is true.

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeFinished || events[0].IsInternal {
		t.Fatalf("expected a non-internal node.finished event, got %+v", events)
	}
	if events[0].Payload["text"] != "the final answer" {
		t.Fatalf("unexpected payload: %+v", events[0].Payload)
	}
}

func TestTranslateEvent_PartialText_IsInternalThinking(t *testing.T) {
	ev := &session.Event{}
	ev.Content = genai.NewContentFromText("partial chunk", genai.RoleModel)
	ev.Partial = true // streaming, not yet the committed final response

	events := TranslateEvent("architect", ev)
	if len(events) != 1 || events[0].Type != EventNodeThinking || !events[0].IsInternal {
		t.Fatalf("expected an internal node.thinking event for partial text, got %+v", events)
	}
}

func TestTranslateEvent_Nil_ReturnsNothing(t *testing.T) {
	if got := TranslateEvent("architect", nil); got != nil {
		t.Fatalf("expected nil for a nil event, got %+v", got)
	}
}
