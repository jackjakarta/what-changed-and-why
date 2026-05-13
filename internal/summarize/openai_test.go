package summarize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubResponse mimics the minimal OpenAI chat-completion response shape used
// by the SDK's UnmarshalJSON; only the fields we read need to be populated.
type stubResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []stubChoice `json:"choices"`
}

type stubChoice struct {
	FinishReason string      `json:"finish_reason"`
	Index        int         `json:"index"`
	Message      stubMessage `json:"message"`
	Logprobs     any         `json:"logprobs"`
}

type stubMessage struct {
	Content string `json:"content"`
	Refusal string `json:"refusal"`
	Role    string `json:"role"`
}

func newStubServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status >= 400 {
			http.Error(w, "boom", status)
			return
		}
		resp := stubResponse{
			ID:      "stub",
			Object:  "chat.completion",
			Created: 1700000000,
			Model:   "stub-model",
			Choices: []stubChoice{{
				FinishReason: "stop",
				Index:        0,
				Message:      stubMessage{Content: content, Role: "assistant"},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestOpenAISummarize(t *testing.T) {
	srv := newStubServer(t, 200, `"hardened JWT expiry tolerance"`)
	defer srv.Close()

	s := New("stub-model", "test-key", srv.URL)
	if s == nil {
		t.Fatal("New returned nil with valid args")
	}
	got, err := s.Summarize(context.Background(), GroupBrief{
		PRNumber: 1, Title: "t", SymbolName: "validateToken",
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got != "hardened JWT expiry tolerance" {
		t.Errorf("got summary %q, want %q", got, "hardened JWT expiry tolerance")
	}
}

func TestOpenAISummarizeError(t *testing.T) {
	srv := newStubServer(t, 500, "")
	defer srv.Close()

	s := New("stub-model", "test-key", srv.URL)
	_, err := s.Summarize(context.Background(), GroupBrief{PRNumber: 1, Title: "t", SymbolName: "f"})
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !strings.Contains(err.Error(), "chat completion") {
		t.Errorf("error message doesn't wrap context: %v", err)
	}
}

func TestOpenAISummarizeEmptyContent(t *testing.T) {
	srv := newStubServer(t, 200, "   \n  ")
	defer srv.Close()

	s := New("stub-model", "test-key", srv.URL)
	_, err := s.Summarize(context.Background(), GroupBrief{PRNumber: 1, Title: "t", SymbolName: "f"})
	if err == nil {
		t.Fatal("expected error when model returns empty content")
	}
}

func TestNewReturnsNilWithoutKey(t *testing.T) {
	if s := New("model", "", "http://x"); s != nil {
		t.Errorf("expected nil summarizer when API key is empty")
	}
	if s := New("", "key", "http://x"); s != nil {
		t.Errorf("expected nil summarizer when model is empty")
	}
}
