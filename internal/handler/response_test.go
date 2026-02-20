package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRespondJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"hello": "world"}

	RespondJSON(rec, http.StatusOK, data)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got["hello"] != "world" {
		t.Errorf("body = %v, want hello=world", got)
	}
}

func TestRespondError(t *testing.T) {
	rec := httptest.NewRecorder()

	RespondError(rec, http.StatusNotFound, "not_found", "resource not found")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var got errorBody
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", got.Error.Code, "not_found")
	}
	if got.Error.Message != "resource not found" {
		t.Errorf("error.message = %q, want %q", got.Error.Message, "resource not found")
	}
}
