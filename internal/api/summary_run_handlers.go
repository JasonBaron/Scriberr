package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"scriberr/internal/repository"
)

// SummaryRunRequest selects recordings and templates for a bulk run. Give
// job_ids, or set one of tag, missing_only or all.
type SummaryRunRequest struct {
	JobIDs      []string `json:"job_ids"`
	Tag         string   `json:"tag"`
	MissingOnly bool     `json:"missing_only"`
	All         bool     `json:"all"`
	// TemplateID is the template to run; empty means the default template.
	TemplateID string `json:"template_id"`
	// IncludeTagTemplates also runs templates linked to each recording's
	// tags (default true).
	IncludeTagTemplates *bool `json:"include_tag_templates"`
	// Force re-runs a template on recordings it has already summarized.
	Force bool `json:"force"`
	// DryRun returns the matching recordings without queuing anything.
	DryRun bool `json:"dry_run"`
}

// SummaryRunResponse reports what a run request queued.
type SummaryRunResponse struct {
	Matched  int      `json:"matched"`
	Queued   int      `json:"queued"`
	JobIDs   []string `json:"job_ids"`
	Template string   `json:"template"`
	DryRun   bool     `json:"dry_run,omitempty"`
}

// StartSummaryRun queues summaries for existing recordings
// @Summary Summarize existing recordings
// @Description Queue summaries for recordings selected by IDs, tag, missing summary, or all. Runs one at a time in the background; poll GET /api/v1/summaries/run.
// @Tags summaries
// @Accept json
// @Produce json
// @Param request body SummaryRunRequest true "Selection"
// @Success 202 {object} SummaryRunResponse
// @Failure 400 {object} map[string]string
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/run [post]
func (h *Handler) StartSummaryRun(c *gin.Context) {
	var req SummaryRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Tag = strings.TrimSpace(strings.ToLower(req.Tag))
	if len(req.JobIDs) == 0 && req.Tag == "" && !req.MissingOnly && !req.All {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Choose recordings: job_ids, tag, missing_only or all"})
		return
	}
	ctx := c.Request.Context()

	tplID := strings.TrimSpace(req.TemplateID)
	tplName := ""
	isDefault := false
	if tplID == "" {
		tpl, err := h.defaultTemplate(ctx)
		if err != nil || tpl == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No default template. Mark one as Default in Settings > Summary, or pass template_id."})
			return
		}
		tplID, tplName, isDefault = tpl.ID, tpl.Name, true
	} else {
		tpl, err := h.summaryRepo.FindByID(ctx, tplID)
		if err != nil || tpl == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Template not found"})
			return
		}
		tplName, isDefault = tpl.Name, tpl.IsDefault
	}

	ids, err := h.summaryRepo.SummaryRunCandidates(ctx, repository.SummaryRunFilter{
		JobIDs: req.JobIDs, Tag: req.Tag, MissingOnly: req.MissingOnly})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to select recordings"})
		return
	}
	resp := SummaryRunResponse{Matched: len(ids), JobIDs: ids, Template: tplName, DryRun: req.DryRun}
	if req.DryRun || len(ids) == 0 {
		c.JSON(http.StatusOK, resp)
		return
	}
	follow := req.IncludeTagTemplates == nil || *req.IncludeTagTemplates
	tasks := make([]summaryTask, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, summaryTask{JobID: id, TemplateID: tplID, Force: req.Force,
			FollowTags: follow && isDefault, Reason: "bulk"})
	}
	resp.Queued = h.queueSummaries(tasks...)
	c.JSON(http.StatusAccepted, resp)
}

// GetSummaryRun reports background summary progress
// @Summary Summary run progress
// @Description Queued, running and finished counts for background summaries, with the most recent results
// @Tags summaries
// @Produce json
// @Success 200 {object} SummaryRunStatus
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/run [get]
func (h *Handler) GetSummaryRun(c *gin.Context) {
	c.JSON(http.StatusOK, h.summaries.status())
}

// CancelSummaryRun drops queued summaries
// @Summary Cancel queued summaries
// @Description Drops every queued summary. The one being written finishes.
// @Tags summaries
// @Produce json
// @Success 200 {object} map[string]int
// @Security ApiKeyAuth
// @Security BearerAuth
// @Router /api/v1/summaries/run [delete]
func (h *Handler) CancelSummaryRun(c *gin.Context) {
	dropped := h.summaries.cancel()
	for _, t := range dropped {
		h.setSummaryStatus(t.JobID, "")
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": len(dropped)})
}
