package adk

import (
	"context"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"github.com/marcon0203/agentic-kit/internal/modelgateway"
)

type fakeGWClient struct {
	content string
	in, out int64
}

func (c *fakeGWClient) Complete(context.Context, string, string, modelgateway.CompletionRequest) (modelgateway.CompletionResult, error) {
	return modelgateway.CompletionResult{Content: c.content, InputTokens: c.in, OutputTokens: c.out}, nil
}

func TestGatewayLLM_GenerateContent(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{
		"anthropic": &fakeGWClient{content: "hello from the model", in: 12, out: 6},
	}, nil)
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"}, nil,
		map[string]string{"anthropic": "sk-test"})

	if llm.Name() != "anthropic/claude-sonnet-5" {
		t.Fatalf("unexpected Name(): %q", llm.Name())
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
	}
	var got *model.LLMResponse
	for resp, err := range llm.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = resp
	}
	if got == nil {
		t.Fatal("expected a response")
	}
	if contentText(got.Content) != "hello from the model" {
		t.Fatalf("unexpected content: %q", contentText(got.Content))
	}
	if got.UsageMetadata.PromptTokenCount != 12 || got.UsageMetadata.CandidatesTokenCount != 6 {
		t.Fatalf("unexpected usage metadata: %+v", got.UsageMetadata)
	}
}

func TestGatewayLLM_GenerateContent_PropagatesError(t *testing.T) {
	gw := modelgateway.NewGatewayWithClients(map[string]modelgateway.Client{}, nil) // no client registered
	llm := NewGatewayLLM(gw, modelgateway.ModelSpec{Provider: "anthropic", Name: "claude-sonnet-5"}, nil, map[string]string{"anthropic": "sk-test"})

	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}
	var gotErr error
	for _, err := range llm.GenerateContent(context.Background(), req, false) {
		gotErr = err
	}
	if gotErr == nil {
		t.Fatal("expected an error when the gateway has no client for the provider")
	}
}

func TestRoleToMessageRole(t *testing.T) {
	cases := map[string]string{string(genai.RoleModel): "assistant", "system": "system", string(genai.RoleUser): "user", "": "user"}
	for in, want := range cases {
		if got := roleToMessageRole(in); got != want {
			t.Errorf("roleToMessageRole(%q) = %q, want %q", in, got, want)
		}
	}
}
