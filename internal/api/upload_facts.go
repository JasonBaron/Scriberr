package api

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"scriberr/internal/mediainfo"
	"scriberr/internal/models"
	"scriberr/pkg/logger"
)

// paramLastModified is the uploaded file's modified time in Unix
// milliseconds (the browser's File.lastModified).
const paramLastModified = "last_modified"

// uploadFacts is what an upload tells us about the recording before any
// conversion: when it was recorded, its hash and size, and its name.
type uploadFacts struct {
	recordedAt       *time.Time
	recordedAtSource string
	hash             string
	size             int64
	originalName     string
}

// inspectUpload reads the saved upload before it is converted (conversion
// drops tags and changes the bytes). The recorded time comes from a date
// tag in the file, else the modified time the client sent.
func inspectUpload(c *gin.Context, path, originalName string) uploadFacts {
	f := uploadFacts{originalName: strings.TrimSpace(filepath.Base(originalName))}
	if f.originalName == "." {
		f.originalName = ""
	}
	if t, ok := mediainfo.RecordedAt(c.Request.Context(), path); ok {
		f.recordedAt, f.recordedAtSource = &t, mediainfo.SourceMetadata
	} else if ms, err := strconv.ParseInt(c.PostForm(paramLastModified), 10, 64); err == nil {
		if t, ok := mediainfo.FromUnixMillis(ms, time.Now()); ok {
			f.recordedAt, f.recordedAtSource = &t, mediainfo.SourceFileTime
		}
	}
	if h, n, err := mediainfo.HashFile(path); err == nil {
		f.hash, f.size = h, n
	} else {
		logger.Warn("Could not hash upload", "path", path, "error", err)
	}
	return f
}

// applyUpload copies the upload facts onto a new job and lists any earlier
// recordings with the same file, so the response can warn about them.
func (h *Handler) applyUpload(ctx context.Context, job *models.TranscriptionJob, f uploadFacts) {
	job.RecordedAt = f.recordedAt
	job.RecordedAtSource = f.recordedAtSource
	job.FileHash = f.hash
	job.FileSize = f.size
	job.OriginalFilename = f.originalName
	job.Duplicates = h.duplicatesOf(ctx, job.ID, f.hash)
	if len(job.Duplicates) > 0 {
		logger.Info("Upload matches an existing recording", "job_id", job.ID, "duplicate_of", job.Duplicates[0].ID)
	}
}

// duplicatesOf lists other recordings whose uploaded file has this hash.
func (h *Handler) duplicatesOf(ctx context.Context, jobID, hash string) []models.DuplicateRef {
	if hash == "" || hash == hashMissing {
		return nil
	}
	jobs, err := h.jobRepo.FindByFileHash(ctx, hash, jobID)
	if err != nil {
		return nil
	}
	var out []models.DuplicateRef
	for _, j := range jobs {
		title := ""
		if j.Title != nil {
			title = *j.Title
		}
		out = append(out, models.DuplicateRef{ID: j.ID, Title: title, CreatedAt: j.CreatedAt})
	}
	return out
}

// ensureRecordedAt fills RecordedAt for jobs uploaded before it existed,
// from the stored audio file's tags. It updates job in place.
func (h *Handler) ensureRecordedAt(ctx context.Context, job *models.TranscriptionJob) {
	if job.RecordedAt != nil || job.AudioPath == "" {
		return
	}
	t, ok := mediainfo.RecordedAt(ctx, job.AudioPath)
	if !ok {
		return
	}
	if err := h.jobRepo.SetRecordedAt(ctx, job.ID, t, mediainfo.SourceMetadata); err != nil {
		return
	}
	job.RecordedAt = &t
	job.RecordedAtSource = mediainfo.SourceMetadata
}

// jobDate is the date used in titles: when it was recorded if known,
// otherwise when it was uploaded.
func jobDate(job *models.TranscriptionJob) time.Time {
	if job.RecordedAt != nil {
		return *job.RecordedAt
	}
	return job.CreatedAt
}
