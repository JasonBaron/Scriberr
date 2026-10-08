package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeOllama serves /api/show with the given context length and records the
// options of each /api/chat request.
func fakeOllama(t *testing.T, ctxLen int) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model_info": map[string]any{"llama.context_length": ctxLen},
			})
		case "/api/chat":
			var req ollamaChatRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			got = append(got, req.Options)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": req.Model, "done": true,
				"message": map[string]any{"role": "assistant", "content": "ok"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any { mu.Lock(); defer mu.Unlock(); return got }
}

func numCtxOf(t *testing.T, opts map[string]any) int {
	t.Helper()
	v, ok := opts["num_ctx"].(float64)
	if !ok {
		t.Fatalf("num_ctx missing from options: %v", opts)
	}
	return int(v)
}

func TestChatSendsNumCtxSizedToPrompt(t *testing.T) {
	t.Setenv(EnvOllamaNumCtx, "")
	t.Setenv(EnvOllamaNumCtxMax, "")
	srv, requests := fakeOllama(t, 131072)
	s := NewOllamaService(srv.URL)
	ctx := context.Background()

	short := []ChatMessage{{Role: "user", Content: "hi"}}
	// ~68 minutes of speech is roughly 55k characters
	long := []ChatMessage{{Role: "system", Content: "Summarize."}, {Role: "user", Content: strings.Repeat("word ", 11000)}}

	if _, err := s.ChatCompletion(ctx, "llama3.1", short, 0.2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChatCompletion(ctx, "llama3.1", long, 0); err != nil {
		t.Fatal(err)
	}
	reqs := requests()
	if got := numCtxOf(t, reqs[0]); got != minNumCtx {
		t.Errorf("short prompt num_ctx = %d, want %d", got, minNumCtx)
	}
	if reqs[0]["temperature"] != 0.2 {
		t.Errorf("temperature not passed: %v", reqs[0])
	}
	got := numCtxOf(t, reqs[1])
	if got < 55000/charsPerToken || got > defaultNumCtxMax || got%2048 != 0 {
		t.Errorf("long prompt num_ctx = %d, want >= %d, <= %d, multiple of 2048", got, 55000/charsPerToken, defaultNumCtxMax)
	}
	if _, ok := reqs[1]["temperature"]; ok {
		t.Errorf("temperature 0 should be omitted: %v", reqs[1])
	}
}

func TestNumCtxCapsAndOverrides(t *testing.T) {
	srv, _ := fakeOllama(t, 16384)
	s := NewOllamaService(srv.URL)
	ctx := context.Background()
	huge := []ChatMessage{{Role: "user", Content: strings.Repeat("x", 500000)}}

	t.Setenv(EnvOllamaNumCtx, "")
	t.Setenv(EnvOllamaNumCtxMax, "")
	if got := s.numCtx(ctx, "small", huge); got != 16384 {
		t.Errorf("capped by model max: got %d, want 16384", got)
	}
	if w, _ := s.GetContextWindow(ctx, "small"); w != 16384 {
		t.Errorf("GetContextWindow = %d, want 16384", w)
	}

	t.Setenv(EnvOllamaNumCtxMax, "12288")
	if got := s.numCtx(ctx, "small", huge); got != 12288 {
		t.Errorf("capped by OLLAMA_NUM_CTX_MAX: got %d, want 12288", got)
	}

	t.Setenv(EnvOllamaNumCtx, "6000")
	if got := s.numCtx(ctx, "small", huge); got != 6000 {
		t.Errorf("fixed OLLAMA_NUM_CTX: got %d, want 6000", got)
	}
	if w, _ := s.GetContextWindow(ctx, "small"); w != 6000 {
		t.Errorf("GetContextWindow with fixed num_ctx = %d, want 6000", w)
	}
}

func TestModelMaxUnknownFallsBackToCap(t *testing.T) {
	t.Setenv(EnvOllamaNumCtx, "")
	t.Setenv(EnvOllamaNumCtxMax, "")
	s := NewOllamaService("http://127.0.0.1:1") // unreachable
	if w, _ := s.GetContextWindow(context.Background(), "m"); w != defaultNumCtxMax {
		t.Errorf("GetContextWindow = %d, want %d", w, defaultNumCtxMax)
	}
}
