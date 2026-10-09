package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"scriberr/internal/models"
	"scriberr/internal/recordings"
	"scriberr/pkg/clock"
	"scriberr/pkg/logger"
)

// SetRecordings turns on per-recording folders. Without it (tests, legacy
// layout) files stay in uploads/ and transcripts/.
func (h *Handler) SetRecordings(store recordings.Store) {
	h.recordings = store
}

// adoptAudio moves a new upload into its recording folder before the job is
// saved, so the stored path is final from the start.
func (h *Handler) adoptAudio(job *models.TranscriptionJob) {
	if !h.recordings.Enabled() || job.IsMultiTrack {
		return
	}
	if p, err := h.recordings.Adopt(job.ID, job.AudioPath); err == nil {
		job.AudioPath = p
	} else {
		logger.Warn("Could not move upload into its recording folder", "job_id", job.ID, "error", err)
	}
}

// SyncRecording rewrites a recording's folder from the database: it moves
// the audio in if it is still elsewhere, and refreshes metadata.json,
// transcript.json and summaries/. Safe to call any time; errors are logged.
func (h *Handler) SyncRecording(ctx context.Context, jobID string) {
	if !h.recordings.Enabled() {
		return
	}
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil {
		return
	}
	if !job.IsMultiTrack && job.AudioPath != "" && !h.recordings.Contains(job.AudioPath) {
		if _, statErr := os.Stat(job.AudioPath); statErr == nil {
			if p, err := h.recordings.Adopt(job.ID, job.AudioPath); err == nil {
				if err := h.jobRepo.SetAudioPath(ctx, job.ID, p); err == nil {
					job.AudioPath = p
				}
			}
		}
	}
	summaries, _ := h.summaryRepo.ListSummaries(ctx, jobID)
	names := h.templateNames(ctx)
	top, files, err := recordings.Mirror(job, summaries, func(id *string) string {
		if id != nil && names[*id] != "" {
			return names[*id]
		}
		return "summary"
	}, clock.Display)
	if err != nil {
		logger.Warn("Could not build recording files", "job_id", jobID, "error", err)
		return
	}
	for name, data := range top {
		if err := h.recordings.WriteFile(jobID, name, data); err != nil {
			logger.Warn("Could not write recording file", "job_id", jobID, "file", name, "error", err)
		}
	}
	if err := h.recordings.SyncDir(jobID, "summaries", files); err != nil {
		logger.Warn("Could not write summaries folder", "job_id", jobID, "error", err)
	}
}

// SyncRecordingAsync is SyncRecording in the background, for hooks.
func (h *Handler) SyncRecordingAsync(jobID string) {
	go h.SyncRecording(context.Background(), jobID)
}

func (h *Handler) templateNames(ctx context.Context) map[string]string {
	out := map[string]string{}
	if items, _, err := h.summaryRepo.List(ctx, 0, 1000); err == nil {
		for _, t := range items {
			out[t.ID] = t.Name
		}
	}
	return out
}

// jobLogPath finds a job's transcription.log in its recording folder, or in
// the old transcripts/ folder for jobs not moved yet.
func (h *Handler) jobLogPath(jobID string) string {
	if h.recordings.Enabled() {
		p := filepath.Join(h.recordings.ProcessingPath(jobID), "transcription.log")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(h.config.TranscriptsDir, jobID, "transcription.log")
}

// MigrateRecordings moves every recording into its own folder:
// uploads/<id>.<ext> becomes recordings/<id>/audio.<ext> and
// transcripts/<id>/ becomes recordings/<id>/processing/. It only renames
// within one filesystem, updates each job's audio path as it goes, and can
// be interrupted and rerun. Each move is recorded in recordings/migration.log.
func (h *Handler) MigrateRecordings(ctx context.Context) {
	if !h.recordings.Enabled() {
		return
	}
	jobs, err := h.jobRepo.ListStorageInfo(ctx)
	if err != nil {
		logger.Warn("Recording folder migration skipped", "error", err)
		return
	}
	start := time.Now()
	var moved, logsMoved, mirrored, failed int
	var lines []string
	note := func(format string, args ...interface{}) {
		lines = append(lines, time.Now().In(clock.Display).Format("2006-01-02 15:04:05")+" "+fmt.Sprintf(format, args...))
	}

	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		changed := false

		// Folder name: <upload date>_<job-id> (plain <job-id> folders from the
		// first version of this layout are renamed)
		if oldDir, newDir, renamed, err := h.recordings.RenameToDated(j.ID, j.CreatedAt); err != nil {
			failed++
			note("FAIL %s rename folder: %v", j.ID, err)
		} else if renamed {
			changed = true
			note("renamed %s %s -> %s", j.ID, oldDir, newDir)
			if strings.HasPrefix(absPath(j.AudioPath), absPath(oldDir)+string(os.PathSeparator)) {
				p := filepath.Join(newDir, strings.TrimPrefix(absPath(j.AudioPath), absPath(oldDir)+string(os.PathSeparator)))
				if err := h.jobRepo.SetAudioPath(ctx, j.ID, p); err == nil {
					j.AudioPath = p
				} else {
					failed++
					note("FAIL %s database update after rename: %v", j.ID, err)
				}
			}
		}

		// Stored paths are absolute so they do not depend on the working folder
		if h.recordings.Contains(j.AudioPath) && !filepath.IsAbs(j.AudioPath) {
			if p := absPath(j.AudioPath); h.jobRepo.SetAudioPath(ctx, j.ID, p) == nil {
				j.AudioPath = p
				changed = true
			}
		}

		// Model output and job log
		legacy := filepath.Join(h.config.TranscriptsDir, j.ID)
		if n, err := recordings.MoveContents(legacy, h.recordings.ProcessingPath(j.ID)); err != nil {
			failed++
			note("FAIL %s processing files: %v", j.ID, err)
			if errors.Is(err, recordings.ErrCrossDevice) {
				break
			}
		} else if n > 0 {
			logsMoved++
			changed = true
			note("moved %s %s -> %s (%d files)", j.ID, legacy, h.recordings.ProcessingPath(j.ID), n)
		}

		// Audio
		if !j.IsMultiTrack && j.AudioPath != "" && !h.recordings.Contains(j.AudioPath) {
			if _, err := os.Stat(j.AudioPath); err == nil {
				p, err := h.recordings.Adopt(j.ID, j.AudioPath)
				if err != nil {
					failed++
					note("FAIL %s audio %s: %v", j.ID, j.AudioPath, err)
					if errors.Is(err, recordings.ErrCrossDevice) {
						break
					}
					continue
				}
				if err := h.jobRepo.SetAudioPath(ctx, j.ID, p); err != nil {
					// Put the file back so the database and disk agree.
					_ = os.Rename(p, j.AudioPath)
					failed++
					note("FAIL %s database update, audio left at %s: %v", j.ID, j.AudioPath, err)
					continue
				}
				moved++
				changed = true
				note("moved %s %s -> %s", j.ID, j.AudioPath, p)
			} else {
				// A previous run may have moved the file but not saved the path.
				p := h.recordings.AudioPath(j.ID, filepath.Ext(j.AudioPath))
				if _, err := os.Stat(p); err == nil {
					if err := h.jobRepo.SetAudioPath(ctx, j.ID, p); err == nil {
						moved++
						changed = true
						note("repaired %s path -> %s", j.ID, p)
					}
				}
			}
		}

		// Copies of the database, for recordings changed now or never written
		if _, err := os.Stat(filepath.Join(h.recordings.Dir(j.ID), "metadata.json")); changed || err != nil {
			h.SyncRecording(ctx, j.ID)
			mirrored++
		}
	}

	if len(lines) > 0 {
		logPath := filepath.Join(h.recordings.Root, "migration.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(strings.Join(lines, "\n") + "\n")
			_ = f.Close()
		}
	}
	if moved+logsMoved+mirrored+failed > 0 {
		logger.Info("Recording folders updated", "audio_moved", moved, "logs_moved", logsMoved,
			"folders_written", mirrored, "failed", failed, "duration", time.Since(start).Round(time.Millisecond),
			"log", filepath.Join(h.recordings.Root, "migration.log"))
	}
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}
