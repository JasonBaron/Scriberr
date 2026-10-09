package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"scriberr/internal/models"
	"scriberr/internal/voiceid"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type voiceProfileResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Samples    int     `json:"samples"`
	Seconds    float64 `json:"seconds"`
	Recordings int64   `json:"recordings"` // recordings this voice is named in
}

type voicesResponse struct {
	Available        bool                   `json:"available"`
	Auto             bool                   `json:"auto"`
	Threshold        float64                `json:"threshold"`
	DefaultThreshold float64                `json:"default_threshold"`
	Profiles         []voiceProfileResponse `json:"profiles"`
	Counts           map[string]int64       `json:"counts"`
	Queue            int                    `json:"queue"`
	Current          string                 `json:"current,omitempty"`
	Done             int                    `json:"done"`
	Failed           int                    `json:"failed"`
	Matching         bool                   `json:"matching"`
}

func (h *Handler) voicesState(c *gin.Context) voicesResponse {
	ctx := c.Request.Context()
	auto, threshold := h.voiceSettings(ctx)
	out := voicesResponse{Available: h.voicesReady(), Auto: auto, Threshold: threshold, DefaultThreshold: voiceid.DefaultThreshold,
		Profiles: []voiceProfileResponse{}, Counts: map[string]int64{}}
	if h.voiceRepo == nil {
		return out
	}
	if profiles, err := h.voiceRepo.ListProfiles(ctx); err == nil {
		for _, p := range profiles {
			out.Profiles = append(out.Profiles, voiceProfileResponse{ID: p.ID, Name: p.Name, Samples: p.Samples, Seconds: p.Seconds,
				Recordings: h.speakerMappingRepo.CountByName(ctx, p.Name)})
		}
	}
	if counts, err := h.voiceRepo.VoiceCounts(ctx); err == nil {
		out.Counts = counts
	}
	r := h.voices
	r.mu.Lock()
	out.Queue, out.Current, out.Done, out.Failed = len(r.tasks), r.current, r.done, r.failed
	r.mu.Unlock()
	r.matchMu.Lock()
	out.Matching = r.matching
	r.matchMu.Unlock()
	return out
}

// GetVoices lists voice profiles, voice settings and scan progress.
// @Router /api/v1/voices [get]
func (h *Handler) GetVoices(c *gin.Context) {
	c.JSON(http.StatusOK, h.voicesState(c))
}

type voiceSettingsRequest struct {
	Auto      *bool    `json:"auto"`
	Threshold *float64 `json:"threshold"`
}

// SaveVoiceSettings sets automatic scans and the match threshold.
// @Router /api/v1/voices/settings [post]
func (h *Handler) SaveVoiceSettings(c *gin.Context) {
	var req voiceSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	s, err := h.summaryRepo.GetSettings(ctx)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
			return
		}
		s = &models.SummarySetting{}
	}
	rematch := false
	if req.Auto != nil {
		s.VoiceAuto = req.Auto
	}
	if req.Threshold != nil {
		t := *req.Threshold
		if t < 0.3 || t > 0.95 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "threshold must be between 0.3 and 0.95"})
			return
		}
		s.VoiceThreshold = &t
		rematch = true
	}
	if err := h.summaryRepo.SaveSettings(ctx, s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save settings"})
		return
	}
	if rematch && h.voiceRepo != nil {
		h.rematchLibrary()
	}
	c.JSON(http.StatusOK, h.voicesState(c))
}

type voiceScanRequest struct {
	JobIDs []string `json:"job_ids"`
	Rescan bool     `json:"rescan"` // all recordings, not only those never scanned
}

// ScanVoices queues voice scans: the listed recordings, or the library.
// @Router /api/v1/voices/scan [post]
func (h *Handler) ScanVoices(c *gin.Context) {
	if !h.voicesReady() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Voice ID is not available on this server"})
		return
	}
	var req voiceScanRequest
	_ = c.ShouldBindJSON(&req)
	ids := req.JobIDs
	if len(ids) == 0 {
		var err error
		if ids, err = h.voiceRepo.ScanCandidates(c.Request.Context(), req.Rescan); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list recordings"})
			return
		}
	}
	queued := 0
	for _, id := range ids {
		if h.queueVoiceScan(voiceTask{JobID: id, Reason: "requested"}) {
			queued++
		}
	}
	c.JSON(http.StatusOK, gin.H{"queued": queued, "state": h.voicesState(c)})
}

// CancelVoiceScans empties the scan queue (the running scan finishes).
// @Router /api/v1/voices/scan [delete]
func (h *Handler) CancelVoiceScans(c *gin.Context) {
	r := h.voices
	r.mu.Lock()
	dropped := r.tasks
	r.tasks = nil
	for _, t := range dropped {
		delete(r.pending, t.JobID)
	}
	r.mu.Unlock()
	if h.voiceRepo != nil {
		for _, t := range dropped {
			_ = h.voiceRepo.SetVoiceStatus(c.Request.Context(), t.JobID, "", "")
			if t.Then != nil {
				go t.Then()
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"cancelled": len(dropped), "state": h.voicesState(c)})
}

// MatchVoices re-applies voice profiles to every scanned recording.
// @Router /api/v1/voices/match [post]
func (h *Handler) MatchVoices(c *gin.Context) {
	if h.voiceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Voice ID is not available on this server"})
		return
	}
	h.rematchLibrary()
	c.JSON(http.StatusOK, h.voicesState(c))
}

type renameVoiceRequest struct {
	Name string `json:"name" binding:"required"`
}

// RenameVoice renames a profile and the speakers it named by voice.
// @Router /api/v1/voices/{id} [patch]
func (h *Handler) RenameVoice(c *gin.Context) {
	if h.voiceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Voice ID is not available on this server"})
		return
	}
	var req renameVoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	ctx := c.Request.Context()
	p, err := h.voiceRepo.GetProfile(ctx, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Voice not found"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if other, err := h.voiceRepo.ProfileByName(ctx, name); err == nil && other.ID != p.ID {
		c.JSON(http.StatusConflict, gin.H{"error": "Another voice already has that name"})
		return
	}
	old := p.Name
	p.Name = name
	if err := h.voiceRepo.SaveProfile(ctx, p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rename voice"})
		return
	}
	if err := h.speakerMappingRepo.RenameVoiceMappings(ctx, p.ID, name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Renamed, but speaker names were not updated"})
		return
	}
	// Names given by hand that taught this voice follow the rename
	if samples, err := h.voiceRepo.ListSamples(ctx, p.ID); err == nil {
		for _, s := range samples {
			_ = h.speakerMappingRepo.RenameSpeaker(ctx, s.TranscriptionJobID, s.Speaker, old, name)
		}
	}
	c.JSON(http.StatusOK, h.voicesState(c))
}

// DeleteVoice removes a profile and the names it gave by voice. Names given
// by hand stay.
// @Router /api/v1/voices/{id} [delete]
func (h *Handler) DeleteVoice(c *gin.Context) {
	if h.voiceRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Voice ID is not available on this server"})
		return
	}
	if err := h.voiceRepo.DeleteProfile(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete voice"})
		return
	}
	c.JSON(http.StatusOK, h.voicesState(c))
}

type speakerVoiceResponse struct {
	Speaker string             `json:"speaker"`
	Seconds float64            `json:"seconds"`
	Matches []voiceMatchResult `json:"matches"` // closest voices, best first
}

type voiceMatchResult struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// GetRecordingVoices shows one recording's voice scan: each speaker's
// closest voices and any problems found.
// @Router /api/v1/transcription/{id}/voices [get]
func (h *Handler) GetRecordingVoices(c *gin.Context) {
	if h.voiceRepo == nil {
		c.JSON(http.StatusOK, gin.H{"available": false})
		return
	}
	ctx := c.Request.Context()
	jobID := c.Param("id")
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Transcription job not found"})
		return
	}
	prints, _ := h.voiceRepo.ListPrints(ctx, jobID)
	profiles, _ := h.voiceRepo.ListProfiles(ctx)
	speakersOut := []speakerVoiceResponse{}
	for _, p := range prints {
		sv := speakerVoiceResponse{Speaker: p.Speaker, Seconds: p.Seconds, Matches: []voiceMatchResult{}}
		for _, pr := range profiles {
			if len(pr.Vector) > 0 {
				sv.Matches = append(sv.Matches, voiceMatchResult{Name: pr.Name, Score: roundf(voiceid.Cosine(p.Vector, pr.Vector))})
			}
		}
		sort.Slice(sv.Matches, func(i, j int) bool { return sv.Matches[i].Score > sv.Matches[j].Score })
		if len(sv.Matches) > 3 {
			sv.Matches = sv.Matches[:3]
		}
		speakersOut = append(speakersOut, sv)
	}
	var problems []voiceid.Problem
	failure := ""
	if job.VoiceIssues != "" {
		if json.Unmarshal([]byte(job.VoiceIssues), &problems) != nil {
			failure = job.VoiceIssues
		}
	}
	_, threshold := h.voiceSettings(ctx)
	c.JSON(http.StatusOK, gin.H{
		"available": h.voicesReady(), "status": job.VoiceStatus, "scanned_at": job.VoiceScannedAt,
		"speakers": speakersOut, "problems": problems, "error": failure, "threshold": threshold,
	})
}

// ScanRecordingVoices queues one recording's voice scan.
// @Router /api/v1/transcription/{id}/voices/scan [post]
func (h *Handler) ScanRecordingVoices(c *gin.Context) {
	if !h.voicesReady() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Voice ID is not available on this server"})
		return
	}
	queued := h.queueVoiceScan(voiceTask{JobID: c.Param("id"), Reason: "requested"})
	c.JSON(http.StatusOK, gin.H{"queued": queued})
}
