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

func textOnlyResponse() messagesResponse {
	return messagesResponse{
		Content: []contentBlock{
			{Type: "text"},
		},
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
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody.Model != defaultModel {
			t.Errorf("model = %q, want %q", reqBody.Model, defaultModel)
		}
		if len(reqBody.Tools) != 1 {
			t.Errorf("tools count = %d, want 1", len(reqBody.Tools))
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

func TestGenerate_RetryOnNoToolUse(t *testing.T) {
	callCount := 0
	suggestions := []Suggestion{
		{GapNumber: 1, Title: "Retry success", Description: "d", Category: "outdoor",
			EnergyLevel: "low", EstimatedCost: "free", Location: "here", Reasoning: "r"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody messagesRequest
		json.NewDecoder(r.Body).Decode(&reqBody)

		if callCount == 1 {
			// First call: return text-only (no tool_use).
			if reqBody.Temperature != 0.9 {
				t.Errorf("first call temperature = %f, want 0.9", reqBody.Temperature)
			}
			json.NewEncoder(w).Encode(textOnlyResponse())
		} else {
			// Retry: should use lower temperature.
			if reqBody.Temperature != 0.5 {
				t.Errorf("retry temperature = %f, want 0.5", reqBody.Temperature)
			}
			json.NewEncoder(w).Encode(toolUseResponse(suggestions))
		}
	}))
	defer srv.Close()

	c := NewWithBaseURL("test-key", srv.URL)
	result, err := c.Generate(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (initial + retry)", callCount)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("got %d suggestions after retry, want 1", len(result.Suggestions))
	}
	if result.Suggestions[0].Title != "Retry success" {
		t.Errorf("Title = %q", result.Suggestions[0].Title)
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
	c := New("")
	if c != nil {
		t.Error("New(\"\") should return nil")
	}
}
