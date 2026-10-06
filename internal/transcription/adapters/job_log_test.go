package adapters

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactingWriterDropsTranscriptLines(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out)

	input := strings.Join([]string{
		"2026-10-06 21:22:44 - whisperx.transcribe - INFO - Performing transcription...",
		"Transcript: [0.031 --> 27.537]  private words here",
		"  Transcript: [28.0 --> 30.0]  indented private words",
		"Transcription: parakeet full text",
		"Transcription complete: 39481 characters total",
		"Result: canary full text",
		"Results saved to: data/temp/x/result.json",
		"",
	}, "\n")

	// Write in awkward chunks to exercise line buffering.
	for i := 0; i < len(input); i += 7 {
		end := i + 7
		if end > len(input) {
			end = len(input)
		}
		if n, err := w.Write([]byte(input[i:end])); err != nil || n != end-i {
			t.Fatalf("write: n=%d err=%v", n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	got := out.String()
	for _, secret := range []string{"private words", "parakeet full text", "canary full text"} {
		if strings.Contains(got, secret) {
			t.Errorf("log still contains %q:\n%s", secret, got)
		}
	}
	for _, keep := range []string{"Performing transcription", "Transcription complete: 39481", "Results saved to:", "4 transcript line(s) omitted"} {
		if !strings.Contains(got, keep) {
			t.Errorf("log missing %q:\n%s", keep, got)
		}
	}
}

func TestRedactingWriterCarriageReturnAndPartialLine(t *testing.T) {
	var out bytes.Buffer
	w := newRedactingWriter(&out)
	_, _ = w.Write([]byte("progress 10%\rprogress 50%\rTranscript: [1 --> 2] hidden"))
	_ = w.Flush()
	got := out.String()
	if !strings.Contains(got, "progress 10%\r") || !strings.Contains(got, "progress 50%\r") {
		t.Errorf("progress lines not forwarded: %q", got)
	}
	if strings.Contains(got, "hidden") {
		t.Errorf("trailing transcript line leaked: %q", got)
	}
}

func TestOpenJobLogOptOut(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvLogTranscripts, "true")
	w, done, err := OpenJobLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("Transcript: [0 --> 1] kept on purpose\n"))
	done()
	b, _ := os.ReadFile(filepath.Join(dir, "transcription.log"))
	if !strings.Contains(string(b), "kept on purpose") {
		t.Errorf("opt-out did not keep transcript: %q", b)
	}
}

func TestSetEnvReplaces(t *testing.T) {
	env := setEnv([]string{"A=1", "HF_TOKEN=old", "B=2"}, "HF_TOKEN", "new")
	joined := strings.Join(env, ",")
	if strings.Contains(joined, "old") || !strings.Contains(joined, "HF_TOKEN=new") || len(env) != 3 {
		t.Errorf("unexpected env: %v", env)
	}
}
