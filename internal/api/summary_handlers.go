package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"scriberr/internal/models"
	"scriberr/internal/titles"
)

type SummaryTemplateRequest struct {
	Name               string   `json:"name" binding:"required,min=1"`
	Description        *string  `json:"description"`
	Model              string   `json:"model" binding:"required,min=1"`
	Prompt             string   `json:"prompt" binding:"required,min=1"`
	IncludeSpeakerInfo *bool    `json:"include_speaker_info"`
	Reasoning          *bool    `json:"reasoning"`
	IsDefault          *bool    `json:"is_default"`
	AutoTags           []string `json:"auto_tags"`
}

// applyTemplateFlags copies the optional flags from a request onto a template.
func applyTemplateFlags(item *models.SummaryTemplate, req SummaryTemplateRequest) {
	if req.IncludeSpeakerInfo != nil {
		item.IncludeSpeakerInfo = *req.IncludeSpeakerInfo
	}
	if req.Reasoning != nil {
		item.Reasoning = *req.Reasoning
	}
	if req.IsDefault != nil {
		item.IsDefault = *req.IsDefault
	}
	if req.AutoTags != nil {
		item.AutoTags = models.StringList(titles.CleanUserTags(req.AutoTags))
	}
}

// SummarySettingsRequest updates the fields that are set and keeps the rest.
type SummarySettingsRequest struct {
	DefaultModel  *string `json:"default_model"`
	AutoSummarize *bool   `json:"auto_summarize"`
}

type SummarySettingsResponse struct {
	DefaultModel  string `json:"default_model"`
	AutoSummarize bool   `json:"auto_summarize"`
}

func settingsResponse(s *models.SummarySetting) SummarySettingsResponse {
	return SummarySettingsResponse{DefaultModel: s.DefaultModel, AutoSummarize: s.AutoSummarize}
}

// ListSummaryTemplates returns all templates
// @Summary List summarization templates
// @Description Get all summarization templates
// @Tags summaries
// @Produce json
// @Success 200 {array} models.SummaryTemplate
// @Security ApiKeyAuth
// @Security BearerAuth
// @Security BearerAuth
// @Router /api/v1/summaries [get]
func (h *Handler) ListSummaryTemplates(c *gin.Context) {
	// TODO: Add pagination support
	items, _, err := h.summaryRepo.List(c.Request.Context(), 0, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch templates"})
		return
	}
	c.JSON(http.StatusOK, items)
}

// CreateSummaryTemplate creates a new template
// @Summary Create summarization template
// @Description Create a new summarization template
// @Tags summaries
// @Accept json
// @Produce json
// @Param request body SummaryTemplateRequest true "Template payload"
// @Success 201 {object} models.SummaryTemplate
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Security BearerAuth
// @Router /api/v1/summaries [post]
func (h *Handler) CreateSummaryTemplate(c *gin.Context) {
	var req SummaryTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item := &models.SummaryTemplate{
		Name:        req.Name,
		Description: req.Description,
		Model:       req.Model,
		Prompt:      req.Prompt,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	applyTemplateFlags(item, req)
	if err := h.summaryRepo.Create(c.Request.Context(), item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create template"})
		return
	}
	if item.IsDefault {
		if err := h.summaryRepo.SetDefaultTemplate(c.Request.Context(), item.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set default template"})
			return
		}
	}
	c.JSON(http.StatusCreated, item)
}

// GetSummaryTemplate fetches one by id
// @Summary Get summarization template
// @Description Get a summarization template by ID
// @Tags summaries
// @Produce json
// @Param id path string true "Template ID"
// @Success 200 {object} models.SummaryTemplate
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Security BearerAuth
// @Router /api/v1/summaries/{id} [get]
func (h *Handler) GetSummaryTemplate(c *gin.Context) {
	id := c.Param("id")
	item, err := h.summaryRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}
	c.JSON(http.StatusOK, item)
}

// UpdateSummaryTemplate updates an existing template
// @Summary Update summarization template
// @Description Update a summarization template by ID
// @Tags summaries
// @Accept json
// @Produce json
// @Param id path string true "Template ID"
// @Param request body SummaryTemplateRequest true "Template payload"
// @Success 200 {object} models.SummaryTemplate
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Security BearerAuth
// @Router /api/v1/summaries/{id} [put]
func (h *Handler) UpdateSummaryTemplate(c *gin.Context) {
	id := c.Param("id")
	var req SummaryTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.summaryRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template not found"})
		return
	}
	item.Name = req.Name
	item.Description = req.Description
	item.Model = req.Model
	item.Prompt = req.Prompt
	applyTemplateFlags(item, req)
	item.UpdatedAt = time.Now()
	if err := h.summaryRepo.Update(c.Request.Context(), item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update template"})
		return
	}
	if item.IsDefault {
		if err := h.summaryRepo.SetDefaultTemplate(c.Request.Context(), item.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set default template"})
			return
		}
	}
	c.JSON(http.StatusOK, item)
}

// DeleteSummaryTemplate deletes a template
// @Summary Delete summarization template
// @Description Delete a summarization template by ID
// @Tags summaries
// @Produce json
// @Param id path string true "Template ID"
// @Success 204 {string} string "No Content"
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Router /api/v1/summaries/{id} [delete]
func (h *Handler) DeleteSummaryTemplate(c *gin.Context) {
	id := c.Param("id")
	if err := h.summaryRepo.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete template"})
		return
	}
	c.Status(http.StatusNoContent)
}

// GetSummarySettings returns the global summary settings (default model)
// @Summary Get summary settings
// @Description Get global summarization settings
// @Tags summaries
// @Produce json
// @Success 200 {object} SummarySettingsResponse
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Router /api/v1/summaries/settings [get]
func (h *Handler) GetSummarySettings(c *gin.Context) {
	s, err := h.summaryRepo.GetSettings(c.Request.Context())
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusOK, SummarySettingsResponse{DefaultModel: ""})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch settings"})
		return
	}
	c.JSON(http.StatusOK, settingsResponse(s))
}

// SaveSummarySettings updates default model (creates row if absent)
// @Summary Save summary settings
// @Description Create or update global summarization settings
// @Tags summaries
// @Accept json
// @Produce json
// @Param request body SummarySettingsRequest true "Settings payload"
// @Success 200 {object} SummarySettingsResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Router /api/v1/summaries/settings [post]
func (h *Handler) SaveSummarySettings(c *gin.Context) {
	var req SummarySettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s, err := h.summaryRepo.GetSettings(c.Request.Context())
	if err != nil {
		if err != gorm.ErrRecordNotFound {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
			return
		}
		s = &models.SummarySetting{}
	}
	if req.DefaultModel != nil {
		s.DefaultModel = *req.DefaultModel
	}
	if req.AutoSummarize != nil {
		s.AutoSummarize = *req.AutoSummarize
	}
	s.UpdatedAt = time.Now()
	if err := h.summaryRepo.SaveSettings(c.Request.Context(), s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		return
	}
	c.JSON(http.StatusOK, settingsResponse(s))
}
