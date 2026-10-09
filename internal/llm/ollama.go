package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OllamaService handles Ollama API interactions
type OllamaService struct {
	baseURL  string
	client   *http.Client
	ctxCache sync.Map // model -> max context length (int)
}

// Ollama runs every request with its default context window (2048 or 4096
// tokens) unless num_ctx is sent, and silently drops whatever does not fit.
// A long transcript then gets summarised from its last few minutes only.
// Each request now sets num_ctx sized to the prompt.
const (
	// EnvOllamaNumCtx forces a fixed num_ctx for every request.
	EnvOllamaNumCtx = "OLLAMA_NUM_CTX"
	// EnvOllamaNumCtxMax caps the automatic size (KV cache uses GPU memory).
	EnvOllamaNumCtxMax = "OLLAMA_NUM_CTX_MAX"

	defaultNumCtxMax = 32768
	minNumCtx        = 8192
	replyHeadroom    = 4096 // tokens left for the model's answer
	charsPerToken    = 3    // conservative; English averages closer to 4
)

func envInt(key string) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v > 0 {
		return v
	}
	return 0
}

// numCtx picks the context window for a request: enough for the prompt plus
// room for the reply, rounded up to 2048, at least minNumCtx, and no more than
// the cap or the model's own maximum.
func (s *OllamaService) numCtx(ctx context.Context, model string, messages []ChatMessage) int {
	if fixed := envInt(EnvOllamaNumCtx); fixed > 0 {
		return fixed
	}
	limit := envInt(EnvOllamaNumCtxMax)
	if limit == 0 {
		limit = defaultNumCtxMax
	}
	if modelMax := s.modelMaxContext(ctx, model); modelMax > 0 && modelMax < limit {
		limit = modelMax
	}

	chars := 0
	for _, m := range messages {
		chars += len(m.Content) + len(m.Role) + 8
	}
	need := chars/charsPerToken + replyHeadroom
	need = (need + 2047) / 2048 * 2048
	if need < minNumCtx {
		need = minNumCtx
	}
	if need > limit {
		need = limit
	}
	return need
}

// modelMaxContext returns the model's trained context length (cached), or 0.
// modelInfo is what Scriberr needs from /api/show, cached per model.
type modelInfo struct {
	contextLength int
	capabilities  []string
}

func (s *OllamaService) info(ctx context.Context, model string) *modelInfo {
	if v, ok := s.ctxCache.Load(model); ok {
		return v.(*modelInfo)
	}
	mi, found := s.showModel(ctx, model)
	if !found {
		return &modelInfo{}
	}
	s.ctxCache.Store(model, mi)
	return mi
}

func (s *OllamaService) modelMaxContext(ctx context.Context, model string) int {
	return s.info(ctx, model).contextLength
}

// SupportsThinking reports whether the model lists the "thinking" capability
// (qwen3, deepseek-r1 and similar). Older Ollama versions do not report
// capabilities; then this is false and no think flag is sent.
func (s *OllamaService) SupportsThinking(ctx context.Context, model string) bool {
	for _, c := range s.info(ctx, model).capabilities {
		if c == "thinking" {
			return true
		}
	}
	return false
}

// thinkFlag returns the think value to send, or nil to leave the model default.
func (s *OllamaService) thinkFlag(ctx context.Context, model string) *bool {
	want, set := ThinkingFrom(ctx)
	if !set || !s.SupportsThinking(ctx, model) {
		return nil
	}
	return &want
}

func (s *OllamaService) requestOptions(ctx context.Context, model string, messages []ChatMessage, temperature float64) map[string]any {
	opts := map[string]any{"num_ctx": s.numCtx(ctx, model, messages)}
	if temperature > 0 {
		opts["temperature"] = temperature
	} else if DeterministicFrom(ctx) {
		opts["temperature"] = 0
		opts["seed"] = DeterministicSeed
	}
	return opts
}

// NewOllamaService creates a new Ollama service
func NewOllamaService(baseURL string) *OllamaService {
	// Normalize base URL: remove trailing slash
	b := strings.TrimRight(baseURL, "/")
	return &OllamaService{
		baseURL: b,
		client:  &http.Client{Timeout: 60 * time.Minute},
	}
}

// Ollama tags response
type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// GetModels retrieves available chat models from Ollama
func (s *OllamaService) GetModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
	}

	var tags ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	out := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out, nil
}

// Ollama chat API payloads
type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Options  map[string]any      `json:"options,omitempty"`
	// Think is only sent to models that report the "thinking" capability;
	// whether other models accept it is not documented.
	Think *bool `json:"think,omitempty"`
}

type ollamaChatResponse struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// ChatCompletion performs a non-streaming chat completion against Ollama
func (s *OllamaService) ChatCompletion(ctx context.Context, model string, messages []ChatMessage, temperature float64) (*ChatResponse, error) {
	// Map to Ollama messages
	msgs := make([]ollamaChatMessage, 0, len(messages))
	for _, m := range messages {
		msgs = append(msgs, ollamaChatMessage(m))
	}
	reqBody := ollamaChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   false,
	}
	reqBody.Options = s.requestOptions(ctx, model, messages, temperature)
	reqBody.Think = s.thinkFlag(ctx, model)
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/api/chat", bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
	}
	var oResp ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	// Map to generic ChatResponse
	cr := &ChatResponse{Model: oResp.Model}
	cr.Choices = []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{{
		Index: 0,
	}}
	cr.Choices[0].Message.Role = oResp.Message.Role
	cr.Choices[0].Message.Content = oResp.Message.Content
	return cr, nil
}

// ChatCompletionStream performs a streaming chat completion against Ollama
func (s *OllamaService) ChatCompletionStream(ctx context.Context, model string, messages []ChatMessage, temperature float64) (<-chan string, <-chan error) {
	contentChan := make(chan string, 100)
	errorChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(errorChan)

		msgs := make([]ollamaChatMessage, 0, len(messages))
		for _, m := range messages {
			msgs = append(msgs, ollamaChatMessage(m))
		}
		reqBody := ollamaChatRequest{Model: model, Messages: msgs, Stream: true}
		reqBody.Options = s.requestOptions(ctx, model, messages, temperature)
		reqBody.Think = s.thinkFlag(ctx, model)

		data, err := json.Marshal(reqBody)
		if err != nil {
			errorChan <- fmt.Errorf("failed to marshal request: %w", err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/api/chat", bytes.NewBuffer(data))
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		// Debug log the request body
		if len(data) < 2000 {
			fmt.Printf("Debug: Ollama request body: %s\n", string(data))
		} else {
			fmt.Printf("Debug: Ollama request body (truncated): %s...\n", string(data[:2000]))
		}

		resp, err := s.client.Do(req)
		if err != nil {
			errorChan <- fmt.Errorf("failed to make request: %w", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			// Ollama streams JSON objects per line
			if strings.TrimSpace(line) == "" {
				continue
			}
			var chunk ollamaChatResponse
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}
			if chunk.Message.Content != "" {
				select {
				case contentChan <- chunk.Message.Content:
				case <-ctx.Done():
					return
				}
			}
			if chunk.Done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading stream: %w", err)
		}
	}()

	return contentChan, errorChan
}

// ollamaShowRequest represents the request to show model info
type ollamaShowRequest struct {
	Name string `json:"name"`
}

// ollamaShowResponse represents the response from show model info
type ollamaShowResponse struct {
	Capabilities []string               `json:"capabilities"`
	ModelInfo    map[string]interface{} `json:"model_info"`
	Details      struct {
		ContextLength int `json:"context_length"` // Some versions return this
	} `json:"details"`
	Parameters string `json:"parameters"`
}

// GetContextWindow returns the context window Scriberr can use for a model:
// the model's maximum, limited by the num_ctx cap (or the fixed value), since
// that is the most Ollama will be asked to hold.
func (s *OllamaService) GetContextWindow(ctx context.Context, model string) (int, error) {
	if fixed := envInt(EnvOllamaNumCtx); fixed > 0 {
		return fixed, nil
	}
	limit := envInt(EnvOllamaNumCtxMax)
	if limit == 0 {
		limit = defaultNumCtxMax
	}
	if n := s.modelMaxContext(ctx, model); n > 0 && n < limit {
		return n, nil
	}
	return limit, nil
}

// showModel asks Ollama for the model's context length and capabilities.
func (s *OllamaService) showModel(ctx context.Context, model string) (*modelInfo, bool) {
	data, err := json.Marshal(ollamaShowRequest{Name: model})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/api/show", bytes.NewBuffer(data))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var showResp ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&showResp); err != nil {
		return nil, false
	}

	mi := &modelInfo{capabilities: showResp.Capabilities}

	// The format varies by Ollama version: model_info["<arch>.context_length"],
	// details.context_length, or "num_ctx N" in the parameters string.
	for k, v := range showResp.ModelInfo {
		if strings.HasSuffix(k, "context_length") {
			if f, ok := v.(float64); ok && f > 0 {
				mi.contextLength = int(f)
				return mi, true
			}
		}
	}
	if showResp.Details.ContextLength > 0 {
		mi.contextLength = showResp.Details.ContextLength
		return mi, true
	}
	for _, line := range strings.Split(showResp.Parameters, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "num_ctx" {
			if n, err := strconv.Atoi(f[1]); err == nil && n > 0 {
				mi.contextLength = n
				return mi, true
			}
		}
	}
	return mi, true
}

// LoadedModels lists models Ollama currently holds in memory (/api/ps).
func (s *OllamaService) LoadedModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/api/ps", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/ps: status %d", resp.StatusCode)
	}
	var ps struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return nil, err
	}
	var names []string
	for _, m := range ps.Models {
		if m.Model != "" {
			names = append(names, m.Model)
		} else if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

// Unload asks Ollama to free a model's memory now (keep_alive 0).
func (s *OllamaService) Unload(ctx context.Context, model string) error {
	data, err := json.Marshal(map[string]any{"model": model, "keep_alive": 0})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/api/generate", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama unload %s: status %d", model, resp.StatusCode)
	}
	return nil
}

// UnloadAll frees every model Ollama has loaded and returns their names.
func (s *OllamaService) UnloadAll(ctx context.Context) ([]string, error) {
	names, err := s.LoadedModels(ctx)
	if err != nil {
		return nil, err
	}
	var unloaded []string
	for _, n := range names {
		if err := s.Unload(ctx, n); err != nil {
			return unloaded, err
		}
		unloaded = append(unloaded, n)
	}
	return unloaded, nil
}
