package api

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"scriberr/internal/mediainfo"
	"scriberr/internal/models"
)

// paramLastModified is the uploaded file's modified time in Unix
// milliseconds (the browser's File.lastModified).
const paramLastModified = "last_modified"

// recordedAtForUpload works out when an upload was recorded: a date tag in
// the file first, then the file's modified time if the client sent it.
// Call it on the saved file before any conversion, which drops the tags.
func recordedAtForUpload(c *gin.Context, path string) (*time.Time, string) {
	if t, ok := mediainfo.RecordedAt(c.Request.Context(), path); ok {
		return &t, mediainfo.SourceMetadata
	}
	if ms, err := strconv.ParseInt(c.PostForm(paramLastModified), 10, 64); err == nil {
		if t, ok := mediainfo.FromUnixMillis(ms, time.Now()); ok {
			return &t, mediainfo.SourceFileTime
		}
	}
	return nil, ""
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
