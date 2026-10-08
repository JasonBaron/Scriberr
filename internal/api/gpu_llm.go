package api

import (
	"context"
	"os"
	"strings"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/llm"
	"scriberr/internal/models"
	"scriberr/internal/titles"
	"scriberr/pkg/clock"
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

// afterSummary runs in the background once a summary stream has closed,
// while the model is still loaded and the GPU lock is held. It asks the same model for a working
// title and tags, stores them as suggestions, and applies the title when the
// current one is a placeholder (a file name, "New Recording", empty).
func (h *Handler) afterSummary(ctx context.Context, req SummarizeRequest, svc llm.Service, summary string, completed bool) {
	if !envEnabled(titles.EnvSuggest) {
		return
	}
	if !completed || strings.TrimSpace(summary) == "" {
		logger.Info("Title suggestion skipped: summary did not complete", "job_id", req.TranscriptionID)
		return
	}
	job, err := h.jobRepo.FindByID(ctx, req.TranscriptionID)
	if err != nil || job == nil {
		logger.Warn("Title suggestion skipped: job not found", "job_id", req.TranscriptionID, "error", err)
		return
	}

	// Short, deterministic call. Thinking is off regardless of the template:
	// a title does not benefit from it and it would only add latency.
	tctx, cancel := context.WithTimeout(llm.WithThinking(context.Background(), false), 90*time.Second)
	defer cancel()
	resp, err := svc.ChatCompletion(tctx, req.Model, []llm.ChatMessage{{Role: "user", Content: titles.Prompt(summary)}}, 0.2)
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		logger.Warn("Title suggestion failed", "job_id", job.ID, "model", req.Model, "error", err)
		return
	}
	topic, tags, err := titles.ParseSuggestion(resp.Choices[0].Message.Content)
	if err != nil {
		logger.Warn("Title suggestion unusable", "job_id", job.ID, "model", req.Model, "error", err)
		return
	}

	h.ensureRecordedAt(ctx, job)
	title := titles.Format(titles.Pattern(), jobDate(job).In(clock.Display), topic)
	current := ""
	if job.Title != nil {
		current = *job.Title
	}
	apply := titles.IsPlaceholder(current, job.AudioPath)
	if err := h.summaryRepo.SaveSuggestions(ctx, job.ID, title, models.StringList(tags), apply); err != nil {
		logger.Warn("Could not save title suggestion", "job_id", job.ID, "error", err)
		return
	}
	logger.Info("Saved title suggestion", "job_id", job.ID, "title", title, "tags", strings.Join(tags, ","), "applied", apply)
}
