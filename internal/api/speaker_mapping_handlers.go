package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"scriberr/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SpeakerMappingRequest represents a speaker mapping update request
type SpeakerMappingRequest struct {
	OriginalSpeaker string `json:"original_speaker" binding:"required"`
	CustomName      string `json:"custom_name" binding:"required"`
}

// SpeakerMappingsUpdateRequest represents a bulk speaker mappings update
type SpeakerMappingsUpdateRequest struct {
	Mappings []SpeakerMappingRequest `json:"mappings" binding:"required"`
	// Resummarize reruns this recording's summaries when the names changed,
	// so they use the new names.
	Resummarize bool `json:"resummarize"`
}

// SpeakerMappingResponse represents a speaker mapping response
type SpeakerMappingResponse struct {
	ID              uint    `json:"id"`
	OriginalSpeaker string  `json:"original_speaker"`
	CustomName      string  `json:"custom_name"`
	Source          string  `json:"source,omitempty"` // "voice" when named by voice match
	Score           float64 `json:"score,omitempty"`
}

func mappingResponses(mappings []models.SpeakerMapping) []SpeakerMappingResponse {
	out := make([]SpeakerMappingResponse, len(mappings))
	for i, m := range mappings {
		out[i] = SpeakerMappingResponse{ID: m.ID, OriginalSpeaker: m.OriginalSpeaker, CustomName: m.CustomName, Source: m.Source, Score: m.Score}
	}
	return out
}

// GetSpeakerMappings retrieves all speaker mappings for a transcription
// @Summary Get speaker mappings for a transcription
// @Description Retrieves all custom speaker names for a transcription job
// @Tags transcription
// @Produce json
// @Param id path string true "Transcription Job ID"
// @Success 200 {array} SpeakerMappingResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Security ApiKeyAuth
// @Router /api/v1/transcription/{id}/speakers [get]
func (h *Handler) GetSpeakerMappings(c *gin.Context) {
	jobID := c.Param("id")

	// Verify the transcription job exists and has diarization enabled
	job, err := h.jobRepo.FindByID(c.Request.Context(), jobID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transcription job not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get transcription job"})
		return
	}

	// Check if diarization was enabled or if this is a multi-track job (which also has speakers)
	// If no speaker info available, return empty array instead of error for graceful frontend handling
	if !job.Diarization && !job.Parameters.Diarize && !job.IsMultiTrack {
		c.JSON(http.StatusOK, []SpeakerMappingResponse{})
		return
	}

	// Get speaker mappings
	mappings, err := h.speakerMappingRepo.ListByJob(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get speaker mappings"})
		return
	}

	c.JSON(http.StatusOK, mappingResponses(mappings))
}

// UpdateSpeakerMappings updates speaker mappings for a transcription
// @Summary Update speaker mappings for a transcription
// @Description Updates or creates custom speaker names for a transcription job
// @Tags transcription
// @Accept json
// @Produce json
// @Param id path string true "Transcription Job ID"
// @Param request body SpeakerMappingsUpdateRequest true "Speaker mappings to update"
// @Success 200 {array} SpeakerMappingResponse
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Security ApiKeyAuth
// @Router /api/v1/transcription/{id}/speakers [post]
func (h *Handler) UpdateSpeakerMappings(c *gin.Context) {
	jobID := c.Param("id")

	var req SpeakerMappingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Verify the transcription job exists and has diarization enabled
	job, err := h.jobRepo.FindByID(c.Request.Context(), jobID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transcription job not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get transcription job"})
		return
	}

	// Check if diarization was enabled or if this is a multi-track job (which also has speakers)
	if !job.Diarization && !job.Parameters.Diarize && !job.IsMultiTrack {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No speaker information available for this transcription"})
		return
	}

	// Convert request to model. A name the voice match gave and the user
	// left as it was stays a voice name; anything else is a name given by
	// hand, which teaches the voice profiles.
	existing, _ := h.speakerMappingRepo.ListByJob(c.Request.Context(), jobID)
	byVoice := map[string]models.SpeakerMapping{}
	for _, m := range existing {
		if m.Source == models.MappingVoice {
			byVoice[m.OriginalSpeaker] = m
		}
	}
	var mappings []models.SpeakerMapping
	for _, mapping := range req.Mappings {
		m := models.SpeakerMapping{
			TranscriptionJobID: jobID,
			OriginalSpeaker:    mapping.OriginalSpeaker,
			CustomName:         strings.TrimSpace(mapping.CustomName),
		}
		// The dialog sends every speaker; one left at its label is unnamed
		if m.CustomName == "" || m.CustomName == m.OriginalSpeaker {
			continue
		}
		if v, ok := byVoice[m.OriginalSpeaker]; ok && v.CustomName == m.CustomName {
			m.Source, m.VoiceProfileID, m.Score = v.Source, v.VoiceProfileID, v.Score
		}
		mappings = append(mappings, m)
	}
	// One person split over two labels: naming a label with the name a voice
	// match gave another label confirms both, so the voice name is kept
	// rather than dropped when the profiles are applied again.
	typed := map[string]bool{}
	for _, m := range mappings {
		if m.Source != models.MappingVoice {
			typed[strings.ToLower(m.CustomName)] = true
		}
	}
	for i := range mappings {
		if mappings[i].Source == models.MappingVoice && typed[strings.ToLower(mappings[i].CustomName)] {
			mappings[i].Source, mappings[i].VoiceProfileID, mappings[i].Score = models.MappingUser, nil, 0
		}
	}
	before := map[string]string{}
	for _, m := range existing {
		before[m.OriginalSpeaker] = m.CustomName
	}
	changed := len(before) != len(mappings)
	for _, m := range mappings {
		if before[m.OriginalSpeaker] != m.CustomName {
			changed = true
		}
	}

	// Update mappings using repository
	if err := h.speakerMappingRepo.UpdateMappings(c.Request.Context(), jobID, mappings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update speaker mappings"})
		return
	}

	// Fetch updated mappings to return
	updatedMappings, err := h.speakerMappingRepo.ListByJob(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch updated mappings"})
		return
	}

	go h.enrollFromMappings(context.Background(), jobID, updatedMappings)
	if changed {
		h.jobNote(jobID, "Speaker names changed: %s", describeMappings(updatedMappings))
		if req.Resummarize {
			if n := h.resummarize(c.Request.Context(), jobID); n > 0 {
				h.jobNote(jobID, "Summaries queued again with the new names: %d", n)
			}
		}
	}
	c.JSON(http.StatusOK, mappingResponses(updatedMappings))
}

func describeMappings(ms []models.SpeakerMapping) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, m.OriginalSpeaker+" = "+m.CustomName)
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// resummarize queues every template that already summarized a recording
// again, the default first (it also refreshes the title, brief and tags).
// Returns how many were queued.
func (h *Handler) resummarize(ctx context.Context, jobID string) int {
	sums, err := h.summaryRepo.ListSummaries(ctx, jobID)
	if err != nil || len(sums) == 0 {
		return 0
	}
	seen := map[string]bool{}
	var tasks []summaryTask
	def, _ := h.defaultTemplate(ctx)
	for _, s := range sums {
		if s.TemplateID == nil || seen[*s.TemplateID] {
			continue
		}
		seen[*s.TemplateID] = true
		t := summaryTask{JobID: jobID, TemplateID: *s.TemplateID, Force: true, Reason: "speaker names changed"}
		if def != nil && def.ID == *s.TemplateID {
			tasks = append([]summaryTask{t}, tasks...)
		} else {
			tasks = append(tasks, t)
		}
	}
	return h.queueSummaries(tasks...)
}
