package api

import (
	"context"
	"os"
	"strings"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/llm"
	"scriberr/pkg/logger"
)

const (
	// EnvUnloadAfterSummary ("false" to disable) unloads the Ollama model as
	// soon as a summary finishes, freeing GPU memory for transcription.
	EnvUnloadAfterSummary = "SCRIBERR_UNLOAD_LLM_AFTER_SUMMARY"
	// EnvUnloadBeforeTranscription ("false" to disable) unloads every Ollama
	// model before a transcription starts.
	EnvUnloadBeforeTranscription = "SCRIBERR_UNLOAD_LLM_BEFORE_TRANSCRIPTION"

	gpuBusyMessage = "The GPU is busy with a transcription. Try again when it finishes."
)

func envEnabled(key string) bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(key)), "false")
}

// tryLocalLLMGPU takes the GPU lock for a local (Ollama) LLM request. Remote
// providers do not use the GPU and always succeed. ok is false if the GPU is
// busy; callers return gpuBusyMessage instead of waiting.
func tryLocalLLMGPU(svc llm.Service) (release func(), ok bool) {
	if _, local := svc.(*llm.OllamaService); !local {
		return func() {}, true
	}
	return gpu.TryAcquire()
}

// unloadAfterSummary frees the summary model's GPU memory when enabled.
func unloadAfterSummary(svc llm.Service, model string) {
	o, local := svc.(*llm.OllamaService)
	if !local || !envEnabled(EnvUnloadAfterSummary) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := o.Unload(ctx, model); err != nil {
		logger.Warn("Could not unload Ollama model after summary", "model", model, "error", err)
		return
	}
	logger.Info("Unloaded Ollama model after summary", "model", model)
}

// UnloadLocalLLM is registered with gpu.OnBeforeTranscription. It unloads
// every model the configured Ollama server holds and returns a note for the
// job log.
func (h *Handler) UnloadLocalLLM(ctx context.Context) string {
	if !envEnabled(EnvUnloadBeforeTranscription) {
		return ""
	}
	svc, _, err := h.getLLMService(ctx)
	if err != nil {
		return ""
	}
	o, local := svc.(*llm.OllamaService)
	if !local {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	names, err := o.UnloadAll(ctx)
	if err != nil {
		return "could not unload Ollama models: " + err.Error()
	}
	if len(names) == 0 {
		return ""
	}
	return "unloaded from Ollama to free GPU memory: " + strings.Join(names, ", ")
}

// afterSummary runs once a summary stream ends, while the model is still
// loaded and the GPU lock is held. Title and tag suggestions hook in here.
func (h *Handler) afterSummary(ctx context.Context, req SummarizeRequest, svc llm.Service, summary string, completed bool) {
}
