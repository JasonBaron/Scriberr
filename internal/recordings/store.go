// Package recordings keeps each recording's files together in one folder:
//
//	<root>/<upload-date>_<job-id>/   e.g. 2026-10-08_96dbeb14-2e36-...
//	  audio.<ext>          the uploaded audio (or its converted copy)
//	  metadata.json        title, tags, brief, dates, hash (copy of the database)
//	  transcript.json      transcript (copy of the database)
//	  summaries/*.md       every saved summary (copies of the database)
//	  processing/          model output and transcription.log
//
// The database stays the source of truth; the copies are rewritten whenever
// it changes, so a folder can be backed up or exported on its own.
package recordings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProcessingDir is the subfolder for model output and the job log.
const ProcessingDir = "processing"

// Store places recording files under Root. A zero Store (Root == "") is
// disabled: every method is a no-op and paths stay where they are.
type Store struct {
	Root string
	// Loc is the zone for the date in folder names (default UTC).
	Loc *time.Location
	// DateOf returns a recording's upload time, for naming a new folder.
	// When it is nil or the job is unknown (a new upload), now is used.
	DateOf func(jobID string) (time.Time, bool)
}

// Enabled reports whether recordings are kept in per-recording folders.
func (s Store) Enabled() bool { return s.Root != "" }

// FolderName is the folder name for a recording uploaded at t:
// <YYYY-MM-DD>_<job-id>, the date in Loc.
func (s Store) FolderName(jobID string, t time.Time) string {
	loc := s.Loc
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("2006-01-02") + "_" + jobID
}

// Dir is the folder for one recording: the existing one if there is one
// (dated, or plain <job-id> from before dated names), otherwise a new
// dated name.
func (s Store) Dir(jobID string) string {
	if existing := s.existingDir(jobID); existing != "" {
		return existing
	}
	t := time.Now()
	if s.DateOf != nil {
		if d, ok := s.DateOf(jobID); ok {
			t = d
		}
	}
	return filepath.Join(s.Root, s.FolderName(jobID, t))
}

func (s Store) existingDir(jobID string) string {
	if !validID(jobID) {
		return ""
	}
	if m, _ := filepath.Glob(filepath.Join(s.Root, "*_"+jobID)); len(m) > 0 {
		return m[0]
	}
	plain := filepath.Join(s.Root, jobID)
	if st, err := os.Stat(plain); err == nil && st.IsDir() {
		return plain
	}
	return ""
}

func validID(jobID string) bool {
	return jobID != "" && jobID != "." && jobID != ".." && !strings.ContainsAny(jobID, "/\\*?[")
}

// RenameToDated moves a recording's folder from a plain <job-id> name (or
// a dated name with a different date) to <date>_<job-id> for uploadedAt.
// It returns the old and new folder; changed is false when nothing moved.
func (s Store) RenameToDated(jobID string, uploadedAt time.Time) (oldDir, newDir string, changed bool, err error) {
	if !s.Enabled() || !validID(jobID) {
		return "", "", false, nil
	}
	oldDir = s.existingDir(jobID)
	newDir = filepath.Join(s.Root, s.FolderName(jobID, uploadedAt))
	if oldDir == "" || oldDir == newDir {
		return oldDir, newDir, false, nil
	}
	if _, err := os.Stat(newDir); err == nil {
		return oldDir, newDir, false, fmt.Errorf("%s already exists", newDir)
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		if isCrossDevice(err) {
			return oldDir, newDir, false, ErrCrossDevice
		}
		return oldDir, newDir, false, err
	}
	return oldDir, newDir, true, nil
}

// ProcessingPath is where model output and the job log go for a recording.
func (s Store) ProcessingPath(jobID string) string {
	return filepath.Join(s.Dir(jobID), ProcessingDir)
}

// Contains reports whether path is inside the store.
func (s Store) Contains(path string) bool {
	if !s.Enabled() || path == "" {
		return false
	}
	root, err1 := filepath.Abs(s.Root)
	p, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// AudioPath is where a recording's audio lives inside its folder.
func (s Store) AudioPath(jobID, ext string) string {
	return filepath.Join(s.Dir(jobID), "audio"+strings.ToLower(ext))
}

// ErrCrossDevice means the move would need a copy between filesystems.
var ErrCrossDevice = errors.New("recordings folder is on a different filesystem")

// Adopt moves an audio file into the recording's folder and returns its
// new path. It only renames, never copies, so it is instant and cannot
// leave a half-written file. A file already inside the store is left alone.
func (s Store) Adopt(jobID, audioPath string) (string, error) {
	if !s.Enabled() || audioPath == "" || s.Contains(audioPath) {
		return audioPath, nil
	}
	dest := s.AudioPath(jobID, filepath.Ext(audioPath))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return audioPath, err
	}
	if _, err := os.Stat(dest); err == nil {
		return audioPath, fmt.Errorf("%s already exists", dest)
	}
	if err := os.Rename(audioPath, dest); err != nil {
		if isCrossDevice(err) {
			return audioPath, ErrCrossDevice
		}
		return audioPath, err
	}
	return dest, nil
}

// MoveContents moves every entry of src into dst (created if needed) and
// removes src when it is empty. Entries that already exist in dst are left
// in src.
func MoveContents(src, dst string) (moved int, err error) {
	entries, err := os.ReadDir(src)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	for _, e := range entries {
		to := filepath.Join(dst, e.Name())
		if _, err := os.Stat(to); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(src, e.Name()), to); err != nil {
			if isCrossDevice(err) {
				return moved, ErrCrossDevice
			}
			return moved, err
		}
		moved++
	}
	_ = os.Remove(src) // only succeeds when empty
	return moved, nil
}

// CheckSameDevice verifies that files can be renamed from dir into the
// store, by moving a small probe file.
func (s Store) CheckSameDevice(dir string) error {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe := filepath.Join(dir, ".scriberr-move-probe")
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
		return err
	}
	defer os.Remove(probe)
	dest := filepath.Join(s.Root, ".scriberr-move-probe")
	if err := os.Rename(probe, dest); err != nil {
		if isCrossDevice(err) {
			return ErrCrossDevice
		}
		return err
	}
	return os.Remove(dest)
}

// WriteFile writes data to name inside the recording's folder, replacing
// the file atomically.
func (s Store) WriteFile(jobID, name string, data []byte) error {
	if !s.Enabled() {
		return nil
	}
	path := filepath.Join(s.Dir(jobID), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SyncDir makes dir (relative to the recording's folder) hold exactly the
// given files: it writes them and removes any other regular file there.
func (s Store) SyncDir(jobID, dir string, files map[string][]byte) error {
	if !s.Enabled() {
		return nil
	}
	full := filepath.Join(s.Dir(jobID), dir)
	if len(files) == 0 {
		if err := os.RemoveAll(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for name, data := range files {
		if err := s.WriteFile(jobID, filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, keep := files[e.Name()]; !keep && e.Type().IsRegular() {
			_ = os.Remove(filepath.Join(full, e.Name()))
		}
	}
	return nil
}

// Remove deletes a recording's folder.
func (s Store) Remove(jobID string) error {
	if !s.Enabled() {
		return nil
	}
	if dir := s.existingDir(jobID); dir != "" {
		return os.RemoveAll(dir)
	}
	return nil
}
