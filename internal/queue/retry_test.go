package queue

import (
	"strings"
	"testing"
)

const envFailure = `single-track processing failed: transcription failed: WhisperX execution failed: exit status 1
Logs:
[2/3] Transcribing + diarizing (whisperx large-v3)...
      ModuleNotFoundError: No module named 'torch._strobelight.cli_function_profiler'
      FAILED 4.2s
======================================
SCRIBERR JOB FAILED
Failed at   : [2/3] Transcribing + diarizing (whisperx large-v3)
Error       : ModuleNotFoundError: No module named 'torch._strobelight.cli_function_profiler'
`

func TestClassifyFailure(t *testing.T) {
	cases := map[string]string{
		envFailure: FailEnvironment,
		"torch.OutOfMemoryError: CUDA out of memory. Tried to allocate 20.00 MiB": FailTransient,
		"WhisperX execution failed: signal: killed":                               FailTransient,
		"requests.exceptions.ConnectionError: Connection reset by peer":           FailTransient,
		"audio file is corrupt: invalid data found when processing input":         FailPermanent,
	}
	for text, want := range cases {
		if got := ClassifyFailure(text); got != want {
			t.Errorf("%q: got %s want %s", text[:40], got, want)
		}
	}
}

func TestFailureSummary(t *testing.T) {
	step, reason := FailureSummary(envFailure)
	if step != "[2/3] Transcribing + diarizing (whisperx large-v3)" {
		t.Errorf("step %q", step)
	}
	if reason != "ModuleNotFoundError: No module named 'torch._strobelight.cli_function_profiler'" {
		t.Errorf("reason %q", reason)
	}
	// Without the job log footer: the Python error line
	_, reason = FailureSummary("failed: exit status 1\nLogs:\nTraceback\n  File x\nRuntimeError: CUDA error: device-side assert")
	if reason != "RuntimeError: CUDA error: device-side assert" {
		t.Errorf("reason %q", reason)
	}
	// Plain error: first line without the log
	_, reason = FailureSummary("audio conversion failed: ffmpeg exit 1 Logs: lots")
	if reason != "audio conversion failed: ffmpeg exit 1" {
		t.Errorf("reason %q", reason)
	}
	_, reason = FailureSummary(strings.Repeat("x", 500))
	if len(reason) > 310 {
		t.Errorf("not truncated: %d", len(reason))
	}
}

func TestRetryDelayAndMax(t *testing.T) {
	if RetryDelay(1).Seconds() != 30 || RetryDelay(2).Minutes() != 2 {
		t.Fatal(RetryDelay(1), RetryDelay(2))
	}
	t.Setenv(EnvTranscribeRetries, "")
	if maxTranscribeRetries() != 2 {
		t.Fatal("default 2")
	}
	t.Setenv(EnvTranscribeRetries, "0")
	if maxTranscribeRetries() != 0 {
		t.Fatal("0 turns retries off")
	}
}
