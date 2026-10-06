package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Filtering behaviour is tested in package joblog; these cover the adapter wrappers.

func TestOpenJobLogRedactsByDefault(t *testing.T) {
	dir := t.TempDir()
	w, done, err := OpenJobLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("Transcript: [0 --> 1] private\nResults saved to: x\n"))
	done()
	b, _ := os.ReadFile(filepath.Join(dir, "transcription.log"))
	if strings.Contains(string(b), "private") || !strings.Contains(string(b), "Results saved to: x") {
		t.Errorf("unexpected log: %q", b)
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
