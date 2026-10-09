package recordings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scriberr/internal/models"
)

func TestAdoptAndContains(t *testing.T) {
	base := t.TempDir()
	uploaded := time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC) // Oct 8 evening in New York, Oct 9 in UTC
	ny, _ := time.LoadLocation("America/New_York")
	s := Store{Root: filepath.Join(base, "recordings"), Loc: ny, DateOf: func(string) (time.Time, bool) { return uploaded, true }}
	uploads := filepath.Join(base, "uploads")
	if err := s.CheckSameDevice(uploads); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(uploads, "job1.M4A")
	if err := os.WriteFile(src, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.Adopt("job1", src)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(s.Root, "2026-10-08_job1", "audio.m4a") {
		t.Errorf("path = %s", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should be gone")
	}
	if !s.Contains(got) || s.Contains(src) || s.Contains(s.Root) {
		t.Error("Contains is wrong")
	}
	again, err := s.Adopt("job1", got)
	if err != nil || again != got {
		t.Errorf("adopting an adopted file: %s %v", again, err)
	}
	if (Store{}).Enabled() {
		t.Error("zero store should be disabled")
	}
}

func TestMoveContents(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "transcripts", "job1")
	dst := filepath.Join(base, "recordings", "job1", ProcessingDir)
	_ = os.MkdirAll(src, 0o755)
	_ = os.WriteFile(filepath.Join(src, "transcription.log"), []byte("log"), 0o600)
	_ = os.WriteFile(filepath.Join(src, "out.json"), []byte("{}"), 0o600)
	n, err := MoveContents(src, dst)
	if err != nil || n != 2 {
		t.Fatalf("moved %d, %v", n, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("empty source folder should be removed")
	}
	if n, err := MoveContents(filepath.Join(base, "nope"), dst); err != nil || n != 0 {
		t.Errorf("missing source: %d %v", n, err)
	}
}

func TestSyncDirAndRemove(t *testing.T) {
	s := Store{Root: t.TempDir()}
	if err := s.SyncDir("j", "summaries", map[string][]byte{"a.md": []byte("a"), "b.md": []byte("b")}); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncDir("j", "summaries", map[string][]byte{"b.md": []byte("b2")}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir("j"), "summaries"))
	if len(entries) != 1 || entries[0].Name() != "b.md" {
		t.Errorf("entries = %v", entries)
	}
	if err := s.Remove("../x"); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("j"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir("j")); !os.IsNotExist(err) {
		t.Error("folder should be removed")
	}
}

func TestMirror(t *testing.T) {
	title, brief, tr := "2026-10-08 Budget Review", "Q4 budget walkthrough.", `{"text":"hi","segments":[]}`
	tpl := "tpl-1"
	created := time.Date(2026, 10, 8, 19, 50, 0, 0, time.UTC)
	job := &models.TranscriptionJob{ID: "job1", Title: &title, Status: models.StatusCompleted, SummaryBrief: &brief,
		Transcript: &tr, Tags: models.StringList{"budget"}, AudioPath: "/data/recordings/job1/audio.m4a", FileHash: "abc", CreatedAt: created}
	sums := []models.Summary{{ID: "1a2b3c4d-xxxx", TemplateID: &tpl, Model: "qwen3:8b", Content: "## Overview\nText", CreatedAt: created}}
	ny, _ := time.LoadLocation("America/New_York")
	top, files, err := Mirror(job, sums, func(id *string) string { return "Default" }, ny)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["2026-10-08_1550_default_1a2b3c4d.md"]; !ok {
		t.Errorf("summary files = %v", files)
	}
	var meta Metadata
	if err := json.Unmarshal(top["metadata.json"], &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Title != title || meta.Brief != brief || meta.AudioFile != "audio.m4a" || len(meta.Summaries) != 1 {
		t.Errorf("meta = %+v", meta)
	}
	if !strings.Contains(string(top["transcript.json"]), `"text": "hi"`) {
		t.Errorf("transcript = %s", top["transcript.json"])
	}
}

func TestRenameToDated(t *testing.T) {
	s := Store{Root: t.TempDir()}
	plain := filepath.Join(s.Root, "job1")
	_ = os.MkdirAll(plain, 0o755)
	_ = os.WriteFile(filepath.Join(plain, "audio.wav"), []byte("a"), 0o600)
	if s.Dir("job1") != plain {
		t.Fatalf("existing plain folder should be found, got %s", s.Dir("job1"))
	}
	up := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	oldDir, newDir, changed, err := s.RenameToDated("job1", up)
	if err != nil || !changed || oldDir != plain || newDir != filepath.Join(s.Root, "2026-10-06_job1") {
		t.Fatalf("rename: %s %s %v %v", oldDir, newDir, changed, err)
	}
	if s.Dir("job1") != newDir {
		t.Errorf("Dir after rename = %s", s.Dir("job1"))
	}
	if _, _, changed, _ := s.RenameToDated("job1", up); changed {
		t.Error("second rename should do nothing")
	}
	if s.Dir("../etc") == "" {
		t.Error("Dir should still return a path for odd IDs")
	}
}
