package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/llm"
	"scriberr/internal/models"
	"scriberr/pkg/logger"
)

const autoSummaryTimeout = 60 * time.Minute

// storedTranscript is the part of a saved transcript the summary prompt uses.
type storedTranscript struct {
	Text     string `json:"text"`
	Segments []struct {
		Text    string `json:"text"`
		Speaker string `json:"speaker"`
	} `json:"segments"`
}

// buildSummaryContent builds the same prompt the web UI sends: the
// transcript (with speaker labels when the template asks for them) followed
// by the template's instructions.
func buildSummaryContent(transcriptJSON string, includeSpeakers bool, names map[string]string, prompt string) (string, error) {
	var t storedTranscript
	if err := json.Unmarshal([]byte(transcriptJSON), &t); err != nil {
		return "", err
	}
	text := t.Text
	label := "Transcript:"
	if includeSpeakers {
		label = "Transcript (with speaker labels - each line is prefixed with [SPEAKER_NAME]):"
		if len(t.Segments) > 0 {
			lines := make([]string, 0, len(t.Segments))
			for _, s := range t.Segments {
				speaker := names[s.Speaker]
				if speaker == "" {
					speaker = s.Speaker
				}
				if speaker == "" {
					speaker = "UNKNOWN"
				}
				lines = append(lines, "["+speaker+"] "+strings.TrimSpace(s.Text))
			}
			text = strings.Join(lines, "\n")
		}
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("transcript is empty")
	}
	return label + "\n" + text + "\n\nInstructions:\n" + prompt, nil
}

// defaultTemplate returns the template marked as default, if any.
func (h *Handler) defaultTemplate(ctx context.Context) (*models.SummaryTemplate, error) {
	items, _, err := h.summaryRepo.List(ctx, 0, 1000)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].IsDefault {
			return &items[i], nil
		}
	}
	return nil, nil
}

// AutoSummarize is registered with queue.OnJobCompleted. When automatic
// summaries are on, it runs the default template on the finished job,
// waiting for the GPU if a local model is used, then suggests a title and
// tags and unloads the model like a summary started from the UI.
func (h *Handler) AutoSummarize(jobID string) {
	ctx := context.Background()
	settings, err := h.summaryRepo.GetSettings(ctx)
	if err != nil || !settings.AutoSummarize {
		return
	}
	tpl, err := h.defaultTemplate(ctx)
	if err != nil || tpl == nil || tpl.Model == "" {
		logger.Warn("Automatic summary skipped: no default template with a model", "job_id", jobID)
		return
	}
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil || job.Transcript == nil {
		logger.Warn("Automatic summary skipped: no transcript", "job_id", jobID)
		return
	}
	names := map[string]string{}
	if tpl.IncludeSpeakerInfo {
		if mappings, err := h.speakerMappingRepo.ListByJob(ctx, jobID); err == nil {
			for _, m := range mappings {
				names[m.OriginalSpeaker] = m.CustomName
			}
		}
	}
	content, err := buildSummaryContent(*job.Transcript, tpl.IncludeSpeakerInfo, names, tpl.Prompt)
	if err != nil {
		logger.Warn("Automatic summary skipped", "job_id", jobID, "error", err)
		return
	}
	svc, _, err := h.getLLMService(ctx)
	if err != nil {
		logger.Warn("Automatic summary skipped: no LLM configured", "job_id", jobID, "error", err)
		return
	}

	// Local models share the GPU with transcription: wait our turn.
	release := func() {}
	if _, local := svc.(*llm.OllamaService); local {
		wctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		r, err := gpu.Acquire(wctx)
		cancel()
		if err != nil {
			logger.Warn("Automatic summary skipped: GPU stayed busy", "job_id", jobID, "error", err)
			return
		}
		release = r
	}
	defer release()

	start := time.Now()
	logger.Info("Automatic summary started", "job_id", jobID, "template", tpl.Name, "model", tpl.Model)
	sctx, cancel := context.WithTimeout(llm.WithThinking(ctx, tpl.Reasoning), autoSummaryTimeout)
	defer cancel()
	resp, err := svc.ChatCompletion(sctx, tpl.Model, []llm.ChatMessage{{Role: "user", Content: content}}, 0.0)
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		logger.Warn("Automatic summary failed", "job_id", jobID, "model", tpl.Model, "error", err)
		unloadAfterSummary(svc, tpl.Model)
		return
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	tplID := tpl.ID
	req := SummarizeRequest{Model: tpl.Model, TranscriptionID: jobID, TemplateID: &tplID}
	h.persistSummary(req, summary)
	logger.Info("Automatic summary saved", "job_id", jobID, "bytes", len(summary), "duration", time.Since(start).Round(time.Second))

	h.afterSummary(ctx, req, svc, summary, summary != "")
	unloadAfterSummary(svc, tpl.Model)
}
