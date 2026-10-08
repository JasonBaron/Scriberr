// Package gpu coordinates GPU use inside one Scriberr instance.
//
// Transcription jobs, summaries and chat all compete for one consumer GPU,
// and an LLM loaded in Ollama can hold most of its memory. This package gives
// them a single lock, lets transcription free the LLM first, and waits for
// free memory when other applications on the host are using the GPU.
//
// The lock is per process: two Scriberr containers on one GPU do not see each
// other's lock, which is what the free-memory wait covers.
package gpu

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// EnvMinFreeMB is the free GPU memory (MiB) a transcription waits for.
	EnvMinFreeMB = "SCRIBERR_GPU_MIN_FREE_MB"
	// EnvWaitMax is how long to wait for that memory before trying anyway
	// (Go duration, e.g. "30m"; "0" disables waiting).
	EnvWaitMax = "SCRIBERR_GPU_WAIT_MAX"

	defaultMinFreeMB = 6000 // large-v3 float16 with diarization peaked at ~5 GB
	defaultWaitMax   = 30 * time.Minute
	pollInterval     = 15 * time.Second
)

var sem = make(chan struct{}, 1)

// Acquire takes the GPU lock, waiting until it is free or ctx is done.
// Call the returned func to release it.
func Acquire(ctx context.Context) (func(), error) {
	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-sem }) }, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}

// TryAcquire takes the lock only if it is free right now.
func TryAcquire() (func(), bool) {
	select {
	case sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-sem }) }, true
	default:
		return func() {}, false
	}
}

// Hook runs before a transcription uses the GPU. It returns a short note for
// the job log ("" for nothing to report).
type Hook func(context.Context) string

var (
	hooksMu sync.Mutex
	hooks   []Hook
)

// OnBeforeTranscription registers work to run after a transcription takes the
// lock and before it uses the GPU, such as unloading Ollama models.
func OnBeforeTranscription(f Hook) {
	hooksMu.Lock()
	hooks = append(hooks, f)
	hooksMu.Unlock()
}

// RunBeforeTranscription runs the registered hooks in order and returns
// their non-empty notes.
func RunBeforeTranscription(ctx context.Context) []string {
	hooksMu.Lock()
	hs := append([]Hook{}, hooks...)
	hooksMu.Unlock()
	var notes []string
	for _, h := range hs {
		if n := h(ctx); n != "" {
			notes = append(notes, n)
		}
	}
	return notes
}

// freeMiB reports free memory on the first GPU; ok is false without nvidia-smi.
var freeMiB = func() (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=memory.free", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, false
	}
	return n, true
}

// FreeMiB returns free memory on the first GPU, if it can be read.
func FreeMiB() (int, bool) { return freeMiB() }

// MinFreeMiB is the configured memory a transcription waits for.
func MinFreeMiB() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvMinFreeMB))); err == nil && v >= 0 {
		return v
	}
	return defaultMinFreeMB
}

// WaitMax is the configured longest wait for free memory.
func WaitMax() time.Duration {
	if v := strings.TrimSpace(os.Getenv(EnvWaitMax)); v != "" {
		if v == "0" {
			return 0
		}
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return defaultWaitMax
}

// WaitResult describes how a wait for memory ended.
type WaitResult struct {
	Measured bool          // nvidia-smi answered
	FreeMiB  int           // free memory when the wait ended
	Waited   time.Duration // time spent waiting
	TimedOut bool          // gave up after maxWait; caller proceeds anyway
}

// WaitForMemory polls until needMiB is free, maxWait passes or ctx ends.
// report is called each time it has to keep waiting.
func WaitForMemory(ctx context.Context, needMiB int, maxWait time.Duration, report func(freeMiB int)) WaitResult {
	start := time.Now()
	for {
		free, ok := freeMiB()
		if !ok {
			return WaitResult{Waited: time.Since(start)}
		}
		if free >= needMiB {
			return WaitResult{Measured: true, FreeMiB: free, Waited: time.Since(start)}
		}
		if time.Since(start) >= maxWait {
			return WaitResult{Measured: true, FreeMiB: free, Waited: time.Since(start), TimedOut: true}
		}
		if report != nil {
			report(free)
		}
		select {
		case <-ctx.Done():
			return WaitResult{Measured: true, FreeMiB: free, Waited: time.Since(start), TimedOut: true}
		case <-time.After(pollInterval):
		}
	}
}
