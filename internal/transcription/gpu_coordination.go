package transcription

import (
	"context"
	"strings"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/joblog"
)

// acquireGPU takes the GPU lock for a transcription, frees what the hooks can
// (Ollama models), and waits for enough free memory when other applications
// are using the GPU. Each step is noted in the job log. Call release when the
// GPU work is done.
func acquireGPU(ctx context.Context, jl *joblog.Log, device string) (release func(), err error) {
	release, ok := gpu.TryAcquire()
	if !ok {
		jl.Note("waiting for the GPU (a summary, chat or another job is using it)")
		start := time.Now()
		if release, err = gpu.Acquire(ctx); err != nil {
			return func() {}, err
		}
		jl.Note("GPU free after %.0fs", time.Since(start).Seconds())
	}

	for _, n := range gpu.RunBeforeTranscription(ctx) {
		jl.Note("%s", n)
	}

	if strings.EqualFold(device, "cpu") {
		return release, nil
	}
	need := gpu.MinFreeMiB()
	var lastNote time.Time
	res := gpu.WaitForMemory(ctx, need, gpu.WaitMax(), func(free int) {
		if time.Since(lastNote) >= time.Minute {
			jl.Note("waiting for GPU memory: %.1f GB free, need %.1f GB (other applications are using the GPU)",
				float64(free)/1024, float64(need)/1024)
			lastNote = time.Now()
		}
	})
	switch {
	case res.TimedOut:
		jl.Note("GPU memory still low after %s (%.1f GB free); starting anyway", res.Waited.Round(time.Second), float64(res.FreeMiB)/1024)
	case res.Measured && res.Waited >= time.Second:
		jl.Note("GPU memory available after %s (%.1f GB free)", res.Waited.Round(time.Second), float64(res.FreeMiB)/1024)
	}
	return release, nil
}
