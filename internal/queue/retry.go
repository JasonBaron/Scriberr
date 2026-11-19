package queue

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"scriberr/internal/models"
	"scriberr/pkg/logger"
)

// Failure kinds decide what happens after a job fails.
const (
	FailEnvironment = "environment" // Python could not import something: repair, then retry once
	FailTransient   = "transient"   // GPU memory, GPU busy, network: retry after a wait
	FailPermanent   = "permanent"   // bad audio, bad settings: no retry
)

// EnvTranscribeRetries sets how many times a transient failure is retried
// (default 2, 0 turns automatic retries off).
const EnvTranscribeRetries = "SCRIBERR_TRANSCRIBE_RETRIES"

var environmentMarkers = []string{
	"ModuleNotFoundError", "ImportError", "No module named", "undefined symbol",
	"cannot open shared object file", "environment still fails to import",
}

var transientMarkers = []string{
	"CUDA out of memory", "OutOfMemoryError", "CUDA error", "CUBLAS_STATUS", "cuDNN error",
	"CUDNN_STATUS", "signal: killed", "Connection reset", "ConnectionError", "Read timed out",
	"Temporary failure in name resolution", "502 Bad Gateway", "503 Service", "504 Gateway",
	"429 Client Error", "GPU stayed busy",
}

// ClassifyFailure sorts a failure by its error text.
func ClassifyFailure(errText string) string {
	for _, m := range environmentMarkers {
		if strings.Contains(errText, m) {
			return FailEnvironment
		}
	}
	for _, m := range transientMarkers {
		if strings.Contains(errText, m) {
			return FailTransient
		}
	}
	return FailPermanent
}

// FailureSummary shortens a failure (which carries the whole job log) to
// the step that failed and the error, for the console and the UI list.
func FailureSummary(errText string) (step, reason string) {
	lines := strings.Split(errText, "\n")
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if v, ok := strings.CutPrefix(t, "Failed at"); ok {
			step = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), ":"))
		}
		if v, ok := strings.CutPrefix(t, "Error       :"); ok && reason == "" {
			reason = strings.TrimSpace(v)
		}
	}
	if reason == "" {
		for i := len(lines) - 1; i >= 0; i-- {
			t := strings.TrimSpace(lines[i])
			head, _, ok := strings.Cut(t, ":")
			if ok && !strings.ContainsAny(head, " \t\"'/") && (strings.HasSuffix(head, "Error") || strings.HasSuffix(head, "Exception")) {
				reason = t
				break
			}
		}
	}
	if reason == "" {
		reason = strings.TrimSpace(lines[0])
		if i := strings.Index(reason, "Logs:"); i > 0 {
			reason = strings.TrimSpace(reason[:i])
		}
	}
	if len(reason) > 300 {
		reason = reason[:300] + "..."
	}
	return step, reason
}

func maxTranscribeRetries() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvTranscribeRetries))); err == nil && v >= 0 {
		return v
	}
	return 2
}

// RetryDelay is how long to wait before retry n (1-based): 30 s, then 2 min.
// A variable so tests can shorten it.
var RetryDelay = func(n int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < n; i++ {
		d *= 4
	}
	return d
}

// retryState tracks automatic retries per job (in memory: a restart starts
// over, and a manual retry resets it).
type retryState struct {
	mu        sync.Mutex
	transient map[string]int
	envTried  map[string]bool

	repairMu     sync.Mutex
	repairedAt   time.Time
	repairErrors []string
	repair       func(ctx context.Context) []error
	notify       func(jobID, msg string)
}

// SetEnvironmentRepair sets what runs when a job fails because Python could
// not import something. It returns the models still broken afterwards.
func (tq *TaskQueue) SetEnvironmentRepair(fn func(ctx context.Context) []error) {
	tq.retries.mu.Lock()
	tq.retries.repair = fn
	tq.retries.mu.Unlock()
}

// SetNotifier sets where retry notes go (the recording's job log).
func (tq *TaskQueue) SetNotifier(fn func(jobID, msg string)) {
	tq.retries.mu.Lock()
	tq.retries.notify = fn
	tq.retries.mu.Unlock()
}

// ResetRetries forgets a job's automatic retries (before a manual retry).
func (tq *TaskQueue) ResetRetries(jobID string) {
	tq.retries.mu.Lock()
	delete(tq.retries.transient, jobID)
	delete(tq.retries.envTried, jobID)
	tq.retries.mu.Unlock()
}

func (tq *TaskQueue) note(jobID, msg string) {
	tq.retries.mu.Lock()
	fn := tq.retries.notify
	tq.retries.mu.Unlock()
	if fn != nil {
		fn(jobID, msg)
	}
}

func (tq *TaskQueue) jobTitle(jobID string) string {
	job, err := tq.jobRepo.FindByID(context.Background(), jobID)
	if err != nil || job == nil {
		return ""
	}
	if job.Title != nil && *job.Title != "" {
		return *job.Title
	}
	return job.OriginalFilename
}

// handleFailure logs a failed job in one readable line, then retries it
// when the failure is one a retry can fix, or marks it failed.
func (tq *TaskQueue) handleFailure(workerID int, jobID string, err error) {
	full := err.Error()
	kind := ClassifyFailure(full)
	step, reason := FailureSummary(full)
	title := tq.jobTitle(jobID)
	logger.Error("Transcription failed", "job_id", jobID, "title", title, "step", step, "error", reason, "kind", kind, "worker_id", workerID)
	logger.Debug("Transcription failure details", "job_id", jobID, "error", full)

	switch kind {
	case FailEnvironment:
		tq.retries.mu.Lock()
		tried := tq.retries.envTried[jobID]
		repair := tq.retries.repair
		if !tried {
			tq.retries.envTried[jobID] = true
		}
		tq.retries.mu.Unlock()
		if !tried && repair != nil {
			tq.markWaiting(jobID, "Repairing the transcription environment, then retrying: "+reason)
			tq.note(jobID, "Retry: the transcription environment is broken ("+reason+"); repairing it and retrying once")
			go tq.repairThenRetry(jobID, repair)
			return
		}
	case FailTransient:
		max := maxTranscribeRetries()
		tq.retries.mu.Lock()
		n := tq.retries.transient[jobID]
		if n < max {
			tq.retries.transient[jobID] = n + 1
		}
		tq.retries.mu.Unlock()
		if n < max {
			wait := RetryDelay(n + 1)
			msg := "Retry " + strconv.Itoa(n+1) + " of " + strconv.Itoa(max) + " in " + wait.String() + ": " + reason
			logger.Warn("Retrying transcription", "job_id", jobID, "title", title, "attempt", n+2, "wait", wait.String(), "error", reason)
			tq.markWaiting(jobID, msg)
			tq.note(jobID, msg)
			time.AfterFunc(wait, func() { tq.requeue(jobID) })
			return
		}
		if max > 0 {
			logger.Error("Transcription still failing; no retries left", "job_id", jobID, "title", title, "retries", max, "error", reason)
			tq.note(jobID, "No retries left after "+strconv.Itoa(max)+": "+reason)
		}
	}
	tq.finishFailed(jobID, full)
	tq.ResetRetries(jobID)
}

func (tq *TaskQueue) finishFailed(jobID, full string) {
	if err := tq.updateJobStatus(jobID, models.StatusFailed); err != nil {
		logger.Error("Failed to update job status", "job_id", jobID, "error", err)
	}
	if err := tq.updateJobError(jobID, full); err != nil {
		logger.Error("Failed to update job error", "job_id", jobID, "error", err)
	}
}

// markWaiting shows a job as pending with the reason it is waiting.
func (tq *TaskQueue) markWaiting(jobID, msg string) {
	_ = tq.updateJobStatus(jobID, models.StatusPending)
	_ = tq.updateJobError(jobID, msg)
}

func (tq *TaskQueue) requeue(jobID string) {
	select {
	case <-tq.ctx.Done():
		return // pending jobs are picked up again on the next start
	default:
	}
	job, err := tq.jobRepo.FindByID(context.Background(), jobID)
	if err != nil || job == nil || job.Status != models.StatusPending {
		return // deleted, cancelled or restarted by hand meanwhile
	}
	if err := tq.EnqueueJob(jobID); err != nil {
		logger.Warn("Could not queue the retry; it stays pending until the next start", "job_id", jobID, "error", err)
	}
}

// repairThenRetry repairs the environments once for every job that failed
// on the same break (a repair in the last 10 minutes counts), then queues
// the job again, or fails it when the repair did not work.
func (tq *TaskQueue) repairThenRetry(jobID string, repair func(ctx context.Context) []error) {
	r := &tq.retries
	r.repairMu.Lock()
	if time.Since(r.repairedAt) > 10*time.Minute {
		logger.Warn("Repairing transcription environments after an import failure")
		errs := repair(tq.ctx)
		r.repairErrors = nil
		for _, e := range errs {
			r.repairErrors = append(r.repairErrors, e.Error())
		}
		r.repairedAt = time.Now()
	}
	stillBroken := append([]string{}, r.repairErrors...)
	r.repairMu.Unlock()

	if len(stillBroken) > 0 {
		msg := "The transcription environment could not be repaired: " + strings.Join(stillBroken, "; ")
		logger.Error("Transcription environment repair failed; not retrying", "job_id", jobID, "error", msg)
		tq.note(jobID, msg)
		tq.finishFailed(jobID, msg)
		return
	}
	tq.note(jobID, "Retry: environment repaired; transcribing again")
	tq.requeue(jobID)
}
