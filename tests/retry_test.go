package tests

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"scriberr/internal/models"
	"scriberr/internal/queue"
	"scriberr/internal/repository"

	"github.com/stretchr/testify/assert"
)

// scriptedProcessor fails a job with the given errors in turn, then succeeds.
type scriptedProcessor struct {
	mu    sync.Mutex
	fails map[string][]error
	runs  map[string]int
}

func (p *scriptedProcessor) ProcessJob(ctx context.Context, jobID string) error {
	return p.ProcessJobWithProcess(ctx, jobID, nil)
}

func (p *scriptedProcessor) ProcessJobWithProcess(ctx context.Context, jobID string, _ func(*exec.Cmd)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runs[jobID]++
	if errs := p.fails[jobID]; len(errs) > 0 {
		p.fails[jobID] = errs[1:]
		return errs[0]
	}
	return nil
}

func (p *scriptedProcessor) count(jobID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.runs[jobID]
}

// newJob creates a job that startup recovery does not pick up (tests queue it).
func newJob(t *testing.T, helper *TestHelper, title string) *models.TranscriptionJob {
	job := helper.CreateTestTranscriptionJob(t, title)
	assert.NoError(t, helper.DB.Model(job).Update("status", models.StatusUploaded).Error)
	return job
}

func waitStatus(t *testing.T, repo repository.JobRepository, jobID string, want models.JobStatus) *models.TranscriptionJob {
	t.Helper()
	var job *models.TranscriptionJob
	for i := 0; i < 200; i++ {
		job, _ = repo.FindByID(context.Background(), jobID)
		if job != nil && job.Status == want {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s (last %v)", jobID, want, job.Status)
	return nil
}

func TestTranscriptionRetries(t *testing.T) {
	helper := NewTestHelper(t, "retry_test.db")
	defer helper.Cleanup()
	repo := repository.NewJobRepository(helper.DB)
	t.Setenv("QUEUE_WORKERS", "1")
	old := queue.RetryDelay
	queue.RetryDelay = func(int) time.Duration { return 10 * time.Millisecond }
	defer func() { queue.RetryDelay = old }()

	envErr := errors.New("WhisperX execution failed\nLogs:\nError       : ModuleNotFoundError: No module named 'torch._x'")
	oom := errors.New("RuntimeError: CUDA out of memory")
	bad := errors.New("invalid data found when processing input")

	envJob := newJob(t, helper, "env")
	oomJob := newJob(t, helper, "oom")
	badJob := newJob(t, helper, "bad")
	stuckJob := newJob(t, helper, "stuck")
	proc := &scriptedProcessor{runs: map[string]int{}, fails: map[string][]error{
		envJob.ID:   {envErr},
		oomJob.ID:   {oom, oom},
		badJob.ID:   {bad},
		stuckJob.ID: {oom, oom, oom, oom},
	}}
	tq := queue.NewTaskQueue(1, proc, repo)
	var repairs int
	var mu sync.Mutex
	tq.SetEnvironmentRepair(func(ctx context.Context) []error {
		mu.Lock()
		repairs++
		mu.Unlock()
		return nil
	})
	var notes []string
	tq.SetNotifier(func(jobID, msg string) { mu.Lock(); notes = append(notes, msg); mu.Unlock() })
	tq.Start()
	defer tq.Stop()

	for _, id := range []string{envJob.ID, oomJob.ID, badJob.ID, stuckJob.ID} {
		assert.NoError(t, tq.EnqueueJob(id))
	}

	waitStatus(t, repo, envJob.ID, models.StatusCompleted)
	assert.Equal(t, 2, proc.count(envJob.ID), "repaired once, then ran again")
	mu.Lock()
	assert.Equal(t, 1, repairs)
	mu.Unlock()

	waitStatus(t, repo, oomJob.ID, models.StatusCompleted)
	assert.Equal(t, 3, proc.count(oomJob.ID), "two retries")

	failed := waitStatus(t, repo, badJob.ID, models.StatusFailed)
	assert.Equal(t, 1, proc.count(badJob.ID), "a permanent failure is not retried")
	assert.Contains(t, *failed.ErrorMessage, "invalid data")

	failed = waitStatus(t, repo, stuckJob.ID, models.StatusFailed)
	assert.Equal(t, 3, proc.count(stuckJob.ID), "first run plus two retries")
	assert.Contains(t, *failed.ErrorMessage, "CUDA out of memory")

	mu.Lock()
	joined := ""
	for _, n := range notes {
		joined += n + "\n"
	}
	mu.Unlock()
	assert.Contains(t, joined, "repairing it and retrying once")
	assert.Contains(t, joined, "Retry 1 of 2")
	assert.Contains(t, joined, "No retries left after 2")
}

func TestEnvironmentRepairFailsJob(t *testing.T) {
	helper := NewTestHelper(t, "retry_test2.db")
	defer helper.Cleanup()
	repo := repository.NewJobRepository(helper.DB)
	t.Setenv("QUEUE_WORKERS", "1")
	job := newJob(t, helper, "env2")
	proc := &scriptedProcessor{runs: map[string]int{}, fails: map[string][]error{
		job.ID: {errors.New("ImportError: libcudnn.so: cannot open shared object file")},
	}}
	tq := queue.NewTaskQueue(1, proc, repo)
	tq.SetEnvironmentRepair(func(ctx context.Context) []error { return []error{errors.New("whisperx: still broken")} })
	tq.Start()
	defer tq.Stop()
	assert.NoError(t, tq.EnqueueJob(job.ID))
	failed := waitStatus(t, repo, job.ID, models.StatusFailed)
	assert.Equal(t, 1, proc.count(job.ID))
	assert.Contains(t, *failed.ErrorMessage, "could not be repaired")
}

func (suite *APIHandlerTestSuite) TestRetryEndpoints() {
	t := suite.T()
	db := suite.helper.DB
	mk := func(title string, status models.JobStatus) *models.TranscriptionJob {
		j := suite.helper.CreateTestTranscriptionJob(t, title)
		msg := "WhisperX failed\nError       : ModuleNotFoundError: x"
		assert.NoError(t, db.Model(j).Updates(map[string]interface{}{"status": status, "error_message": msg}).Error)
		return j
	}
	a, b := mk("a", models.StatusFailed), mk("b", models.StatusFailed)
	done := mk("done", models.StatusCompleted)

	w := suite.makeAuthenticatedRequest("POST", "/api/v1/transcription/"+done.ID+"/retry", nil, false)
	assert.Equal(t, 400, w.Code, "only failed recordings")

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/transcription/"+a.ID+"/retry", nil, false)
	assert.Equal(t, 200, w.Code)
	var got models.TranscriptionJob
	db.First(&got, "id = ?", a.ID)
	assert.Equal(t, models.StatusPending, got.Status)
	assert.Nil(t, got.ErrorMessage)
	assert.Equal(t, "base", got.Parameters.Model, "same settings")

	w = suite.makeAuthenticatedRequest("POST", "/api/v1/transcription/retry-failed", map[string]interface{}{}, false)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"queued":1`, "only b is still failed")
	db.First(&got, "id = ?", b.ID)
	assert.Equal(t, models.StatusPending, got.Status)
}
