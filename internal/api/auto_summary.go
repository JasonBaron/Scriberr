package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

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
// summaries are on, it queues the default template for the finished job;
// once that summary has tagged the recording, templates linked to its tags
// are queued too.
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
	h.queueSummaries(summaryTask{JobID: jobID, TemplateID: tpl.ID, FollowTags: true, Reason: "automatic"})
}

func (h *Handler) setSummaryStatus(jobID, status string) {
	if err := h.summaryRepo.SetSummaryStatus(context.Background(), jobID, status); err != nil {
		logger.Warn("Could not update summary status", "job_id", jobID, "error", err)
	}
}

// ResetSummaryStatuses clears automatic summaries that a restart cut short.
func (h *Handler) ResetSummaryStatuses(ctx context.Context) {
	if n, err := h.summaryRepo.ClearSummaryStatuses(ctx); err == nil && n > 0 {
		logger.Info("Cleared interrupted automatic summaries", "count", n)
	}
}
