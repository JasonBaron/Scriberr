package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/llm"
	"scriberr/internal/models"
	"scriberr/internal/titles"
	"scriberr/pkg/logger"
)

// summaryTask is one summary to write: a recording and a template.
type summaryTask struct {
	JobID      string `json:"job_id"`
	TemplateID string `json:"template_id"`
	// FollowTags queues the templates linked to the recording's tags once
	// this summary (and the tags it produces) is saved.
	FollowTags bool `json:"-"`
	// Force writes a summary even if one from this template exists.
	Force bool `json:"-"`
	// Retag reruns only the title, brief and tags step on the recording's
	// existing summary; no new summary is written.
	Retag bool `json:"retag,omitempty"`
	// IncludeEdited lets a retag replace tags edited by hand.
	IncludeEdited bool   `json:"-"`
	Reason        string `json:"reason"`
}

func (t summaryTask) same(o summaryTask) bool {
	return t.JobID == o.JobID && t.TemplateID == o.TemplateID && t.Retag == o.Retag
}

// SummaryRunItem is a finished task, for progress reporting.
type SummaryRunItem struct {
	JobID      string    `json:"job_id"`
	TemplateID string    `json:"template_id"`
	Template   string    `json:"template"`
	Status     string    `json:"status"` // done, skipped, failed
	Error      string    `json:"error,omitempty"`
	FinishedAt time.Time `json:"finished_at"`
}

// SummaryRunStatus is what GET /api/v1/summaries/run returns.
type SummaryRunStatus struct {
	Running   bool             `json:"running"`
	Queued    int              `json:"queued"`
	Done      int              `json:"done"`
	Skipped   int              `json:"skipped"`
	Failed    int              `json:"failed"`
	Current   *summaryTask     `json:"current,omitempty"`
	StartedAt *time.Time       `json:"started_at,omitempty"`
	Recent    []SummaryRunItem `json:"recent"`
}

// summaryRunner writes summaries one at a time in the background. Local
// models take the GPU lock per summary, so a transcription can run between
// two summaries of a long batch. The queue lives in memory; summary statuses
// left by a restart are cleared at startup.
type summaryRunner struct {
	mu        sync.Mutex
	cond      *sync.Cond
	queue     []summaryTask
	current   *summaryTask
	done      int
	skipped   int
	failed    int
	startedAt *time.Time
	recent    []SummaryRunItem
	started   bool
}

func newSummaryRunner() *summaryRunner {
	r := &summaryRunner{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

const recentLimit = 50

// enqueue adds tasks that are not already queued or running. It returns how
// many were added.
func (r *summaryRunner) enqueue(tasks ...summaryTask) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) == 0 && r.current == nil {
		now := time.Now()
		r.startedAt = &now
		r.done, r.skipped, r.failed = 0, 0, 0
	}
	added := 0
	for _, t := range tasks {
		if r.has(t) {
			continue
		}
		r.queue = append(r.queue, t)
		added++
	}
	r.cond.Signal()
	return added
}

func (r *summaryRunner) has(t summaryTask) bool {
	if r.current != nil && r.current.same(t) {
		return true
	}
	for _, q := range r.queue {
		if q.same(t) {
			return true
		}
	}
	return false
}

// cancel drops every queued task (not the one running) and returns them.
func (r *summaryRunner) cancel() []summaryTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	dropped := r.queue
	r.queue = nil
	return dropped
}

func (r *summaryRunner) queuedFor(jobID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.queue {
		if q.JobID == jobID {
			return true
		}
	}
	return false
}

func (r *summaryRunner) status() SummaryRunStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := SummaryRunStatus{Running: r.current != nil || len(r.queue) > 0, Queued: len(r.queue),
		Done: r.done, Skipped: r.skipped, Failed: r.failed, StartedAt: r.startedAt,
		Recent: append([]SummaryRunItem{}, r.recent...)}
	if r.current != nil {
		c := *r.current
		s.Current = &c
	}
	return s
}

func (r *summaryRunner) next() summaryTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.queue) == 0 {
		r.cond.Wait()
	}
	t := r.queue[0]
	r.queue = r.queue[1:]
	r.current = &t
	return t
}

func (r *summaryRunner) finish(item SummaryRunItem) (idle bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch item.Status {
	case "done":
		r.done++
	case "skipped":
		r.skipped++
	default:
		r.failed++
	}
	item.FinishedAt = time.Now()
	r.recent = append([]SummaryRunItem{item}, r.recent...)
	if len(r.recent) > recentLimit {
		r.recent = r.recent[:recentLimit]
	}
	r.current = nil
	return len(r.queue) == 0
}

// StartSummaryWorker starts the background summary writer. Call once.
func (h *Handler) StartSummaryWorker(ctx context.Context) {
	r := h.summaries
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()
	go func() {
		var lastSvc llm.Service
		var lastModel string
		for {
			t := r.next()
			if ctx.Err() != nil {
				return
			}
			item, svc, model := h.runSummaryTask(ctx, t)
			if svc != nil {
				lastSvc, lastModel = svc, model
			}
			if idle := r.finish(item); idle && lastSvc != nil {
				// Batch finished: free the GPU memory.
				unloadAfterSummary(lastSvc, lastModel)
				lastSvc = nil
			}
		}
	}()
}

var errSkip = errors.New("skip")

// runSummaryTask writes one summary. The default template's summary also
// produces the title, brief and tags; FollowTags then queues the templates
// linked to those tags.
func (h *Handler) runSummaryTask(ctx context.Context, t summaryTask) (SummaryRunItem, llm.Service, string) {
	item := SummaryRunItem{JobID: t.JobID, TemplateID: t.TemplateID, Status: "done"}
	fail := func(err error) (SummaryRunItem, llm.Service, string) {
		if errors.Is(err, errSkip) {
			item.Status = "skipped"
		} else {
			item.Status = "failed"
			item.Error = err.Error()
			logger.Warn("Summary failed", "job_id", t.JobID, "template_id", t.TemplateID, "reason", t.Reason, "error", err)
		}
		if !h.summaries.queuedFor(t.JobID) {
			h.setSummaryStatus(t.JobID, "")
		}
		return item, nil, ""
	}

	if t.Retag {
		return h.runRetagTask(ctx, t, item, fail)
	}
	tpl, err := h.summaryRepo.FindByID(ctx, t.TemplateID)
	if err != nil || tpl == nil || tpl.Model == "" {
		return fail(errors.New("template missing or has no model"))
	}
	item.Template = tpl.Name
	if !t.Force {
		if exists, _ := h.summaryRepo.HasTemplateSummary(ctx, t.JobID, t.TemplateID); exists {
			if t.FollowTags {
				h.queueTagTemplates(ctx, t.JobID, t.Reason)
			}
			return fail(errSkip)
		}
	}
	job, err := h.jobRepo.FindByID(ctx, t.JobID)
	if err != nil || job == nil || job.Transcript == nil {
		return fail(errors.New("recording has no transcript"))
	}
	names := map[string]string{}
	if tpl.IncludeSpeakerInfo {
		if mappings, err := h.speakerMappingRepo.ListByJob(ctx, t.JobID); err == nil {
			for _, m := range mappings {
				names[m.OriginalSpeaker] = m.CustomName
			}
		}
	}
	content, err := buildSummaryContent(*job.Transcript, tpl.IncludeSpeakerInfo, names, tpl.Prompt)
	if err != nil {
		return fail(err)
	}
	content = h.preparePrompt(ctx, content)
	svc, _, err := h.getLLMService(ctx)
	if err != nil {
		return fail(errors.New("no LLM configured"))
	}

	release := func() {}
	if _, local := svc.(*llm.OllamaService); local {
		wctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		r, err := gpu.Acquire(wctx)
		cancel()
		if err != nil {
			return fail(errors.New("GPU stayed busy"))
		}
		release = r
	}
	defer release()

	h.setSummaryStatus(t.JobID, models.SummaryRunning)
	start := time.Now()
	logger.Info("Summary started", "job_id", t.JobID, "template", tpl.Name, "model", tpl.Model, "reason", t.Reason)
	sctx, cancel := context.WithTimeout(llm.WithDeterministic(llm.WithThinking(ctx, tpl.Reasoning)), autoSummaryTimeout)
	defer cancel()
	resp, err := svc.ChatCompletion(sctx, tpl.Model, []llm.ChatMessage{{Role: "user", Content: content}}, 0.0)
	if err != nil || resp == nil || len(resp.Choices) == 0 {
		if err == nil {
			err = errors.New("empty reply")
		}
		return fail(err)
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if summary == "" {
		return fail(errors.New("empty summary"))
	}
	tplID := tpl.ID
	req := SummarizeRequest{Model: tpl.Model, TranscriptionID: t.JobID, TemplateID: &tplID}
	h.persistSummary(req, summary)
	logger.Info("Summary saved", "job_id", t.JobID, "template", tpl.Name, "bytes", len(summary), "duration", time.Since(start).Round(time.Second))

	// Title, brief and tags come from the default template's summary.
	if tpl.IsDefault {
		h.afterSummary(ctx, req, svc, summary, true)
	}
	if t.FollowTags {
		h.queueTagTemplates(ctx, t.JobID, t.Reason)
	}
	if !h.summaries.queuedFor(t.JobID) {
		h.setSummaryStatus(t.JobID, "")
	}
	h.SyncRecording(ctx, t.JobID)
	return item, svc, tpl.Model
}

// runRetagTask reruns the title, brief and tags step on the summary from
// the default template (or the newest summary when there is none), using
// the default template's model. The template is not run again.
func (h *Handler) runRetagTask(ctx context.Context, t summaryTask, item SummaryRunItem,
	fail func(error) (SummaryRunItem, llm.Service, string)) (SummaryRunItem, llm.Service, string) {
	item.Template = "Retag"
	tpl, err := h.summaryRepo.FindByID(ctx, t.TemplateID)
	if err != nil || tpl == nil || tpl.Model == "" {
		return fail(errors.New("default template missing or has no model"))
	}
	sums, err := h.summaryRepo.ListSummaries(ctx, t.JobID)
	if err != nil || len(sums) == 0 {
		return fail(errSkip)
	}
	pick := newestSummary(sums, tpl.ID)
	svc, _, err := h.getLLMService(ctx)
	if err != nil {
		return fail(errors.New("no LLM configured"))
	}
	release := func() {}
	if _, local := svc.(*llm.OllamaService); local {
		wctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		r, err := gpu.Acquire(wctx)
		cancel()
		if err != nil {
			return fail(errors.New("GPU stayed busy"))
		}
		release = r
	}
	defer release()
	h.setSummaryStatus(t.JobID, models.SummaryRunning)
	start := time.Now()
	if err := h.suggestFromSummary(ctx, t.JobID, tpl.Model, svc, pick.Content, t.IncludeEdited); err != nil {
		return fail(err)
	}
	logger.Info("Retagged recording", "job_id", t.JobID, "model", tpl.Model, "duration", time.Since(start).Round(time.Second))
	if t.FollowTags {
		h.queueTagTemplates(ctx, t.JobID, t.Reason)
	}
	if !h.summaries.queuedFor(t.JobID) {
		h.setSummaryStatus(t.JobID, "")
	}
	h.SyncRecording(ctx, t.JobID)
	return item, svc, tpl.Model
}

// newestSummary returns the newest summary from templateID, or the newest
// of all when that template has none.
func newestSummary(sums []models.Summary, templateID string) models.Summary {
	var best, bestTpl *models.Summary
	for i := range sums {
		s := &sums[i]
		if best == nil || s.CreatedAt.After(best.CreatedAt) {
			best = s
		}
		if s.TemplateID != nil && *s.TemplateID == templateID && (bestTpl == nil || s.CreatedAt.After(bestTpl.CreatedAt)) {
			bestTpl = s
		}
	}
	if bestTpl != nil {
		return *bestTpl
	}
	return *best
}

// queueTagTemplates queues every non-default template linked to one of the
// recording's tags that has not summarized it yet.
func (h *Handler) queueTagTemplates(ctx context.Context, jobID, reason string) int {
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil || len(job.Tags) == 0 {
		return 0
	}
	tpls, _, err := h.summaryRepo.List(ctx, 0, 1000)
	if err != nil {
		return 0
	}
	var tasks []summaryTask
	for _, tpl := range tpls {
		if tpl.IsDefault || !tpl.IsEnabled() || !matchesAnyTag(tpl.AutoTags, job.Tags) {
			continue
		}
		if exists, _ := h.summaryRepo.HasTemplateSummary(ctx, jobID, tpl.ID); exists {
			continue
		}
		tasks = append(tasks, summaryTask{JobID: jobID, TemplateID: tpl.ID, Reason: reason + " (tag)"})
	}
	return h.queueSummaries(tasks...)
}

func matchesAnyTag(want, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if titles.TagMatches(w, h) {
				return true
			}
		}
	}
	return false
}

// queueSummaries queues tasks and marks their recordings as queued.
func (h *Handler) queueSummaries(tasks ...summaryTask) int {
	added := h.summaries.enqueue(tasks...)
	for _, t := range tasks {
		h.setSummaryStatus(t.JobID, models.SummaryQueued)
	}
	return added
}
