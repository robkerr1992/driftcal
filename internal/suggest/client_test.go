package suggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func toolUseResponse(suggestions []Suggestion) messagesResponse {
	toolInput, _ := json.Marshal(map[string]any{"suggestions": suggestions})
	return messagesResponse{
		Content: []contentBlock{
			{Type: "tool_use", Name: "suggest_activities", Input: toolInput},
		},
		Usage: struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}{InputTokens: 500, OutputTokens: 200},
	}
}

func TestGenerate_Success(t *testing.T) {
	suggestions := []Suggestion{
		{
			GapNumber:     1,
			Title:         "Walk in the park",
			Description:   "A nice stroll",
			Category:      "outdoor",
			EnergyLevel:   "medium",
			EstimatedCost: "free",
			Location:      "Central Park",
			Reasoning:     "Good weather",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing anthropic-version header")
		}

		var reqBody messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if reqBody.Model != defaultModel {
			t.Errorf("model = %q, want %q", reqBody.Model, defaultModel)
		}
		if len(reqBody.Tools) != 1 {
			t.Errorf("tools count = %d, want 1", len(reqBody.Tools))
		}
		// Verify tool_choice forces tool use.
		if reqBody.ToolChoice == nil {
			t.Fatal("tool_choice should be set")
		}
		if reqBody.ToolChoice.Type != "tool" {
			t.Errorf("tool_choice.type = %q, want tool", reqBody.ToolChoice.Type)
		}
		if reqBody.ToolChoice.Name != "suggest_activities" {
			t.Errorf("tool_choice.name = %q, want suggest_activities", reqBody.ToolChoice.Name)
		}

		w.Header().Set("x-request-id", "req-123")
		json.NewEncoder(w).Encode(toolUseResponse(suggestions))
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-key", srv.URL)
	result, err := c.Generate(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(result.Suggestions) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(result.Suggestions))
	}
	if result.Suggestions[0].Title != "Walk in the park" {
		t.Errorf("Title = %q", result.Suggestions[0].Title)
	}
	if result.RequestID != "req-123" {
		t.Errorf("RequestID = %q, want req-123", result.RequestID)
	}
	if result.InputTokens != 500 {
		t.Errorf("InputTokens = %d, want 500", result.InputTokens)
	}
}

func TestGenerate_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"too many requests"}}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-key", srv.URL)
	_, err := c.Generate(context.Background(), "system", "user")
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestNew_EmptyKey(t *testing.T) {
	c := New("", "")
	if c != nil {
		t.Error("New(\"\", \"\") should return nil")
	}
}

func TestNew_CustomModel(t *testing.T) {
	c := New("test-key", "claude-haiku-4-5-20251001")
	if c == nil {
		t.Fatal("New should return non-nil client")
	}
	if c.model != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %q, want claude-haiku-4-5-20251001", c.model)
	}
}

func TestNew_DefaultModel(t *testing.T) {
	c := New("test-key", "")
	if c == nil {
		t.Fatal("New should return non-nil client")
	}
	if c.model != defaultModel {
		t.Errorf("model = %q, want %q", c.model, defaultModel)
	}
}
