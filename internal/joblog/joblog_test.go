package joblog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real output captured from WhisperX and PyAnnote runs.
var whisperxOutput = strings.Join([]string{
	"warning: The `--native-tls` flag is deprecated and will be removed in a future release. Use `--system-certs` instead.",
	"2026-10-06 19:47:47 - whisperx.vads.pyannote - INFO - Performing voice activity detection using Pyannote...",
	"Lightning automatically upgraded your loaded checkpoint from v1.5.4 to v2.5.5. To apply the upgrade to your files permanently, run `python -m lightning.pytorch.utilities.upgrade_checkpoint whisperx-env/WhisperX/whisperx/assets/pytorch_model.bin`",
	"2026-10-06 19:47:48 - whisperx.transcribe - INFO - Performing transcription...",
	"/app/whisperx-env/WhisperX/.venv/lib/python3.10/site-packages/pyannote/audio/utils/reproducibility.py:74: ReproducibilityWarning: TensorFloat-32 (TF32) has been disabled as it might lead to reproducibility issues and lower accuracy.",
	"It can be re-enabled by calling",
	"   >>> import torch",
	"   >>> torch.backends.cuda.matmul.allow_tf32 = True",
	"   >>> torch.backends.cudnn.allow_tf32 = True",
	"See https://github.com/pyannote/pyannote-audio/issues/1370 for more details.",
	"",
	"  warnings.warn(",
	"Transcript: [0.031 --> 27.537]  private words here",
	"2026-10-06 19:47:59 - whisperx.transcribe - INFO - Performing alignment...",
	"2026-10-06 19:48:01 - whisperx.transcribe - WARNING - No --hf_token provided, needs to be saved in environment variable, otherwise will throw error loading diarization model",
	"2026-10-06 19:48:01 - whisperx.transcribe - INFO - Performing diarization...",
	"2026-10-06 19:48:01 - whisperx.transcribe - INFO - Using model: pyannote/speaker-diarization-3.1",
	"/app/whisperx-env/WhisperX/.venv/lib/python3.10/site-packages/pyannote/audio/models/blocks/pooling.py:103: UserWarning: std(): degrees of freedom is <= 0. Correction should be strictly less than the reduction factor (input numel divided by output numel). (Triggered internally at /pytorch/aten/src/ATen/native/ReduceOps.cpp:1839.)",
	"  std = sequences.std(dim=-1, correction=1)",
	"",
}, "\n")

var pyannoteFailure = strings.Join([]string{
	"Transcribing:   0%|          | 0/1 [00:00<?, ?it/s]\rTranscribing: 100%|██████████| 1/1 [00:01<00:00,  1.14s/it]",
	"Transcription: parakeet full text that must not leak",
	"Transcription complete: 39481 characters total",
	"/app/whisperx-env/pyannote/.venv/lib/python3.10/site-packages/pyannote/audio/core/io.py:47: UserWarning: ",
	"torchcodec is not installed correctly so built-in audio decoding will fail. Solutions are:",
	"libnvrtc.so.13: cannot open shared object file: No such file or directory",
	"  warnings.warn(",
	"Pipeline loaded successfully",
	"Error during diarization: name 'AudioDecoder' is not defined",
	"",
}, "\n")

func writeChunked(t *testing.T, w *outputWriter, s string) {
	t.Helper()
	for i := 0; i < len(s); i += 7 {
		end := i + 7
		if end > len(s) {
			end = len(s)
		}
		if n, err := w.Write([]byte(s[i:end])); err != nil || n != end-i {
			t.Fatalf("write: n=%d err=%v", n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestOutputFiltersNoiseAndTranscripts(t *testing.T) {
	var out bytes.Buffer
	dir := t.TempDir()
	w := newOutputWriter(&out, dir, false, false)
	tick := w.start
	w.now = func() time.Time { tick = tick.Add(2 * time.Second); return tick }
	writeChunked(t, w, whisperxOutput)
	got := out.String()

	for _, gone := range []string{"private words", "`--native-tls` flag", "Lightning automatically", "TensorFloat-32", ">>> import torch", "warnings.warn", "No --hf_token", "degrees of freedom", "sequences.std", "2026-10-06 19:47"} {
		if strings.Contains(got, gone) {
			t.Errorf("output still contains %q:\n%s", gone, got)
		}
	}
	for _, keep := range []string{
		"      - voice activity detection using Pyannote (+2.0s)",
		"      - transcription (+4.0s)",
		"      - alignment (+6.0s)",
		"      - diarization (+8.0s)",
		"      Using model: pyannote/speaker-diarization-3.1",
		"1 transcript line(s) omitted",
		"5 known warning(s) suppressed (TF32, hf_token flag, lightning checkpoint, pyannote std(), uv native-tls)",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("output missing %q:\n%s", keep, got)
		}
	}
	if e, _ := takeOutcome(dir); e != "" {
		t.Errorf("unexpected error recorded on a clean run: %q", e)
	}
}

func TestOutputVerboseKeepsRawButStillRedacts(t *testing.T) {
	var out bytes.Buffer
	w := newOutputWriter(&out, t.TempDir(), false, true)
	writeChunked(t, w, whisperxOutput)
	got := out.String()
	if !strings.Contains(got, "TensorFloat-32") || !strings.Contains(got, "2026-10-06 19:47:47 - whisperx") {
		t.Errorf("verbose mode should keep raw output:\n%s", got)
	}
	if strings.Contains(got, "private words") {
		t.Errorf("verbose mode leaked transcript text")
	}
}

func TestOutputKeepTranscripts(t *testing.T) {
	var out bytes.Buffer
	w := newOutputWriter(&out, t.TempDir(), true, false)
	writeChunked(t, w, whisperxOutput)
	if !strings.Contains(out.String(), "private words here") {
		t.Errorf("keepText should keep transcript lines:\n%s", out.String())
	}
}

func TestFailureFooterShowsRealErrorAndHint(t *testing.T) {
	dir := t.TempDir()
	l := Open(dir, "job-123")
	tick := l.start
	l.now = func() time.Time { tick = tick.Add(time.Second); return tick }
	l.Header(Field{"Title", "BENCH"}, Field{"Empty", ""})
	l.Section("PRE-FLIGHT", Field{"HF token", "present (env)"})
	l.SetTotalStages(3)
	l.SetAudioSeconds(300)
	l.Stage("Preparing audio")
	l.Done("300.0 s, wav")
	l.Stage("Diarizing (pyannote community-1)")

	w, done, err := OpenOutput(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(pyannoteFailure))
	done()

	l.Finish(errors.New("single-track processing failed: diarization failed: PyAnnote execution failed: exit status 1\nLogs:\n...huge dump..."))

	b, _ := os.ReadFile(filepath.Join(dir, LogFileName))
	got := string(b)
	for _, keep := range []string{
		"SCRIBERR JOB START",
		"Job ID      : job-123",
		"Title       : BENCH",
		"PRE-FLIGHT\n",
		"HF token    : present (env)",
		"[1/3] Preparing audio...",
		"      done  1.0s  (300.0 s, wav)",
		"[2/3] Diarizing (pyannote community-1)...",
		"      Transcription complete: 39481 characters total",
		"      FAILED ",
		"SCRIBERR JOB FAILED",
		"Failed at   : [2/3] Diarizing (pyannote community-1)",
		"Error       : Error during diarization: name 'AudioDecoder' is not defined",
		"Hint        : torchcodec failed to load",
		"Status      : FAILED",
		"(RTF ",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("log missing %q:\n%s", keep, got)
		}
	}
	for _, gone := range []string{"Empty", "parakeet full text", "huge dump", "it/s]"} {
		if strings.Contains(got, gone) {
			t.Errorf("log should not contain %q:\n%s", gone, got)
		}
	}
}

func TestSuccessFooterAndNilSafety(t *testing.T) {
	var nilLog *Log
	nilLog.Header()
	nilLog.Stage("x")
	nilLog.Done("")
	nilLog.Finish(nil) // must not panic

	dir := t.TempDir()
	l := Open(dir, "job-ok")
	l.Stage("Saving transcript")
	l.Finish(nil)
	b, _ := os.ReadFile(filepath.Join(dir, LogFileName))
	got := string(b)
	for _, keep := range []string{"[1/1] Saving transcript...", "      done  ", "SCRIBERR JOB COMPLETE", "Status      : SUCCESS"} {
		if !strings.Contains(got, keep) {
			t.Errorf("log missing %q:\n%s", keep, got)
		}
	}
	if strings.Contains(got, "Error") {
		t.Errorf("success footer should not have an Error line:\n%s", got)
	}
}

func TestOpenOutputEnvOptOut(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLogTranscripts, "true")
	w, done, err := OpenOutput(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("Transcript: [0 --> 1] kept on purpose\n"))
	done()
	b, _ := os.ReadFile(filepath.Join(dir, LogFileName))
	if !strings.Contains(string(b), "kept on purpose") {
		t.Errorf("opt-out did not keep transcript: %q", b)
	}
}
