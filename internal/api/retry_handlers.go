package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"scriberr/internal/models"
	"scriberr/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var errNotRetryable = errors.New("not retryable")

// retryJob queues a failed recording again with the settings it last ran
// with. A missing recording or one that is not failed is errNotRetryable.
func (h *Handler) retryJob(ctx context.Context, jobID, why string) error {
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil {
		return errNotRetryable
	}
	if job.Status != models.StatusFailed || strings.HasPrefix(job.ID, "track_") {
		return errNotRetryable
	}
	if job.Parameters.ModelFamily == "" && job.Parameters.Model == "" {
		return errors.New("this recording has no saved settings; start it from Transcribe instead")
	}
	job.Status = models.StatusPending
	job.Transcript = nil
	job.Summary = nil
	job.ErrorMessage = nil
	if err := h.jobRepo.Update(ctx, job); err != nil {
		return err
	}
	h.taskQueue.ResetRetries(jobID)
	if err := h.taskQueue.EnqueueJob(jobID); err != nil {
		// Stays pending: queued again on the next start
		logger.Warn("Retry saved but not queued", "job_id", jobID, "error", err)
	}
	h.jobNote(jobID, "Retry requested (%s); transcribing again with the same settings", why)
	logger.Info("Transcription retry queued", "job_id", jobID, "reason", why)
	return nil
}

// RetryTranscription queues one failed recording again with its saved settings.
// @Summary Retry a failed transcription
// @Tags transcription
// @Param id path string true "Job ID"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/transcription/{id}/retry [post]
// @Security ApiKeyAuth
// @Security BearerAuth
func (h *Handler) RetryTranscription(c *gin.Context) {
	jobID := c.Param("id")
	if _, err := h.jobRepo.FindByID(c.Request.Context(), jobID); errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	switch err := h.retryJob(c.Request.Context(), jobID, "by hand"); {
	case errors.Is(err, errNotRetryable):
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only failed recordings can be retried"})
	case err != nil:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusOK, gin.H{"queued": 1})
	}
}

type retryFailedRequest struct {
	JobIDs []string `json:"job_ids"` // empty: every failed recording
}

// RetryFailedTranscriptions queues failed recordings again: the listed ones,
// or all of them.
// @Summary Retry failed transcriptions
// @Tags transcription
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/transcription/retry-failed [post]
// @Security ApiKeyAuth
// @Security BearerAuth
func (h *Handler) RetryFailedTranscriptions(c *gin.Context) {
	var req retryFailedRequest
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	ids := req.JobIDs
	if len(ids) == 0 {
		failed, err := h.jobRepo.FindByStatus(ctx, models.StatusFailed)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list failed recordings"})
			return
		}
		for _, j := range failed {
			ids = append(ids, j.ID)
		}
	}
	queued, skipped := 0, 0
	var problems []string
	for _, id := range ids {
		switch err := h.retryJob(ctx, id, "retry failed"); {
		case err == nil:
			queued++
		case errors.Is(err, errNotRetryable):
			skipped++
		default:
			skipped++
			problems = append(problems, id+": "+err.Error())
		}
	}
	c.JSON(http.StatusOK, gin.H{"queued": queued, "skipped": skipped, "problems": problems})
}
