package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"scriberr/internal/recordings"
	"scriberr/pkg/clock"
	"scriberr/pkg/logger"
)

// ExportRecording streams a zip of one recording
// @Summary Export a recording as a zip
// @Description Audio, metadata.json, transcript.json, summaries/*.md and the job log, in a folder named like the recording's folder
// @Tags transcription
// @Produce application/zip
// @Param id path string true "Transcription ID"
// @Success 200 {file} binary
// @Failure 404 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/transcription/{id}/export [get]
func (h *Handler) ExportRecording(c *gin.Context) {
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

	// Always build the copies fresh from the database, so the export is
	// current even if the folder has not been rewritten yet.
	summaries, _ := h.summaryRepo.ListSummaries(ctx, job.ID)
	names := h.templateNames(ctx)
	top, sums, err := recordings.Mirror(job, summaries, func(id *string) string {
		if id != nil && names[*id] != "" {
			return names[*id]
		}
		return "summary"
	}, clock.Display)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build export"})
		return
	}

	folder := filepath.Base(h.recordings.Dir(job.ID))
	if !h.recordings.Enabled() {
		folder = recordings.Store{Loc: clock.Display}.FolderName(job.ID, job.CreatedAt)
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, folder))
	c.Status(http.StatusOK)

	zw := zip.NewWriter(c.Writer)
	defer zw.Close()
	mod := time.Now()

	addBytes := func(name string, data []byte) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: folder + "/" + name, Method: zip.Deflate, Modified: mod})
		if err == nil {
			_, _ = w.Write(data)
		}
	}
	addFile := func(name, path string, method uint16) {
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			return
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: folder + "/" + name, Method: method, Modified: st.ModTime()})
		if err != nil {
			return
		}
		if _, err := io.Copy(w, f); err != nil {
			logger.Warn("Export interrupted", "job_id", job.ID, "file", name, "error", err)
		}
	}

	// Audio is already compressed (or large PCM); store it as is.
	if job.AudioPath != "" {
		addFile("audio"+filepath.Ext(job.AudioPath), job.AudioPath, zip.Store)
	}
	for name, data := range top {
		addBytes(name, data)
	}
	for name, data := range sums {
		addBytes("summaries/"+name, data)
	}
	addFile("transcription.log", h.jobLogPath(job.ID), zip.Deflate)
}
