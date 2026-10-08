package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"scriberr/internal/models"
	"scriberr/internal/titles"
)

// UpdateTagsRequest replaces a recording's tags.
type UpdateTagsRequest struct {
	Tags []string `json:"tags"`
}

// ListTags returns every tag in use with how many recordings have it
// @Summary List tags
// @Description All tags in use, most used first
// @Tags transcription
// @Produce json
// @Success 200 {array} repository.TagCount
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/tags [get]
func (h *Handler) ListTags(c *gin.Context) {
	counts, err := h.summaryRepo.TagCounts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list tags"})
		return
	}
	c.JSON(http.StatusOK, counts)
}

// UpdateTranscriptionTags replaces a recording's tags
// @Summary Update tags
// @Description Replace a recording's tags. Edited tags are no longer overwritten by summaries.
// @Tags transcription
// @Accept json
// @Produce json
// @Param id path string true "Transcription ID"
// @Param request body UpdateTagsRequest true "Tags"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/transcription/{id}/tags [put]
func (h *Handler) UpdateTranscriptionTags(c *gin.Context) {
	var req UpdateTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tags := titles.CleanUserTags(req.Tags)
	err := h.summaryRepo.SetTags(c.Request.Context(), c.Param("id"), models.StringList(tags))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tags"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "tags": tags})
}
