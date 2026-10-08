package queue

import "sync"

var (
	completedMu    sync.RWMutex
	completedHooks []func(jobID string)
)

// OnJobCompleted registers fn to run after a job finishes successfully and
// its status is saved. Each hook runs in its own goroutine, so a slow hook
// (an automatic summary) never holds up the next job.
func OnJobCompleted(fn func(jobID string)) {
	completedMu.Lock()
	defer completedMu.Unlock()
	completedHooks = append(completedHooks, fn)
}

func runCompletedHooks(jobID string) {
	completedMu.RLock()
	hooks := append([]func(string){}, completedHooks...)
	completedMu.RUnlock()
	for _, fn := range hooks {
		go fn(jobID)
	}
}
