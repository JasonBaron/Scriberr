package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"scriberr/internal/mediainfo"
	"scriberr/internal/models"
	"scriberr/pkg/logger"
)

// hashMissing marks recordings whose file was gone when hashes were filled
// in, so the backfill does not retry them on every start.
const hashMissing = "missing"

// FileInfoResponse describes a recording's stored file.
type FileInfoResponse struct {
	JobID            string                `json:"job_id"`
	OriginalFilename string                `json:"original_filename,omitempty"`
	StoredFilename   string                `json:"stored_filename"`
	FileExists       bool                  `json:"file_exists"`
	StoredSize       int64                 `json:"stored_size,omitempty"`
	UploadedSize     int64                 `json:"uploaded_size,omitempty"`
	SHA256           string                `json:"sha256,omitempty"`
	HashOf           string                `json:"hash_of,omitempty"` // "upload" or "stored file"
	Duplicates       []models.DuplicateRef `json:"duplicates"`
	RecordedAt       *time.Time            `json:"recorded_at,omitempty"`
	RecordedAtSource string                `json:"recorded_at_source,omitempty"`
	UploadedAt       time.Time             `json:"uploaded_at"`
	Media            *mediainfo.Info       `json:"media,omitempty"`
	ProbeError       string                `json:"probe_error,omitempty"`
}

// GetFileInfo returns file metadata for a recording
// @Summary Get file metadata
// @Description Format, codec, duration, embedded tags, dates, SHA-256 and duplicate recordings for a job's audio file
// @Tags transcription
// @Produce json
// @Param id path string true "Transcription ID"
// @Success 200 {object} FileInfoResponse
// @Failure 404 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/transcription/{id}/file-info [get]
func (h *Handler) GetFileInfo(c *gin.Context) {
	ctx := c.Request.Context()
	job, err := h.jobRepo.FindByID(ctx, c.Param("id"))
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && job == nil) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get job"})
		return
	}

	resp := FileInfoResponse{
		JobID:            job.ID,
		OriginalFilename: job.OriginalFilename,
		StoredFilename:   filepath.Base(job.AudioPath),
		UploadedSize:     job.FileSize,
		SHA256:           job.FileHash,
		RecordedAt:       job.RecordedAt,
		RecordedAtSource: job.RecordedAtSource,
		UploadedAt:       job.CreatedAt,
	}
	if job.FileHash == hashMissing {
		job.FileHash, resp.SHA256 = "", ""
	}
	if job.FileHash != "" {
		resp.HashOf = "upload"
	}
	if st, err := os.Stat(job.AudioPath); err == nil && !st.IsDir() {
		resp.FileExists = true
		resp.StoredSize = st.Size()
		if job.FileHash == "" {
			if hash, size, err := mediainfo.HashFile(job.AudioPath); err == nil {
				_ = h.jobRepo.SetFileHash(ctx, job.ID, hash, size)
				resp.SHA256, resp.UploadedSize, resp.HashOf = hash, size, "stored file"
			}
		}
		if info, err := mediainfo.Probe(ctx, job.AudioPath); err == nil {
			resp.Media = info
		} else {
			resp.ProbeError = "Could not read media details"
		}
	}
	resp.Duplicates = h.duplicatesOf(ctx, job.ID, resp.SHA256)
	if resp.Duplicates == nil {
		resp.Duplicates = []models.DuplicateRef{}
	}
	c.JSON(http.StatusOK, resp)
}

// BackfillFileHashes hashes the stored audio of recordings uploaded before
// hashes were recorded, so duplicate checks cover them. It runs once in the
// background at startup, a batch at a time, and stops when none are left.
func (h *Handler) BackfillFileHashes(ctx context.Context) {
	done := 0
	for {
		jobs, err := h.jobRepo.ListMissingFileHash(ctx, 50)
		if err != nil || len(jobs) == 0 {
			break
		}
		progress := false
		for _, j := range jobs {
			if ctx.Err() != nil {
				return
			}
			hash, size, err := mediainfo.HashFile(j.AudioPath)
			if err != nil {
				// Missing file: mark it so it is not retried every batch.
				hash, size = hashMissing, 0
			}
			if err := h.jobRepo.SetFileHash(ctx, j.ID, hash, size); err == nil {
				progress = true
				done++
			}
		}
		if !progress {
			break
		}
	}
	if done > 0 {
		logger.Info("Hashed existing recordings", "count", done)
	}
}
