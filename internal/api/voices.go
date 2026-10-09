package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"scriberr/internal/gpu"
	"scriberr/internal/models"
	"scriberr/internal/repository"
	"scriberr/internal/transcription/adapters"
	"scriberr/internal/transcription/interfaces"
	"scriberr/internal/transcription/speakers"
	"scriberr/internal/voiceid"
	"scriberr/pkg/logger"

	"gorm.io/gorm"
)

// VoicePrinter embeds each speaker's turns in an audio file.
type VoicePrinter interface {
	VoicePrints(ctx context.Context, audioPath string, turns []adapters.VoiceTurn, logW io.Writer) (*adapters.VoicePrintResult, error)
}

// SetVoices turns on voice ID: prints are made by printer and kept in repo.
func (h *Handler) SetVoices(repo repository.VoiceRepository, printer VoicePrinter) {
	h.voiceRepo = repo
	h.voicePrinter = printer
}

func (h *Handler) voicesReady() bool { return h.voiceRepo != nil && h.voicePrinter != nil }

type voiceTask struct {
	JobID  string
	Reason string
	Then   func()
	First  bool // new recordings go ahead of a library scan
}

// voiceRunner scans recordings one at a time, in the order queued.
type voiceRunner struct {
	mu      sync.Mutex
	tasks   []voiceTask
	pending map[string]bool
	wake    chan struct{}
	current string
	done    int
	failed  int

	matchMu      sync.Mutex
	matching     bool
	matchAgain   bool
	lastMatched  int
	lastMatchErr string
}

func newVoiceRunner() *voiceRunner {
	return &voiceRunner{pending: map[string]bool{}, wake: make(chan struct{}, 1)}
}

func (r *voiceRunner) push(t voiceTask) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending[t.JobID] {
		return false
	}
	r.pending[t.JobID] = true
	if t.First {
		i := 0
		for i < len(r.tasks) && r.tasks[i].First {
			i++
		}
		r.tasks = append(r.tasks[:i], append([]voiceTask{t}, r.tasks[i:]...)...)
	} else {
		r.tasks = append(r.tasks, t)
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return true
}

func (r *voiceRunner) pop() (voiceTask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tasks) == 0 {
		return voiceTask{}, false
	}
	t := r.tasks[0]
	r.tasks = r.tasks[1:]
	r.current = t.JobID
	return t, true
}

func (r *voiceRunner) finish(jobID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, jobID)
	r.current = ""
	if err != nil {
		r.failed++
	} else {
		r.done++
	}
}

// StartVoiceWorker resets scans a restart cut short and starts the scanner.
func (h *Handler) StartVoiceWorker(ctx context.Context) {
	if !h.voicesReady() {
		return
	}
	if n, err := h.voiceRepo.ClearStaleVoiceStatuses(ctx); err == nil && n > 0 {
		logger.Info("Cleared interrupted voice scans", "count", n)
	}
	go func() {
		for {
			t, ok := h.voices.pop()
			if !ok {
				select {
				case <-ctx.Done():
					return
				case <-h.voices.wake:
					continue
				}
			}
			err := h.scanVoices(ctx, t.JobID)
			h.voices.finish(t.JobID, err)
			if t.Then != nil {
				t.Then()
			}
		}
	}()
}

// queueVoiceScan queues a recording. Reports false when it was already
// queued (Then is then run right away).
func (h *Handler) queueVoiceScan(t voiceTask) bool {
	if !h.voicesReady() {
		if t.Then != nil {
			go t.Then()
		}
		return false
	}
	if !h.voices.push(t) {
		if t.Then != nil {
			go t.Then()
		}
		return false
	}
	_ = h.voiceRepo.SetVoiceStatus(context.Background(), t.JobID, models.VoiceQueued, "")
	return true
}

func (h *Handler) voiceSettings(ctx context.Context) (auto bool, threshold float64) {
	s := h.summarySettings(ctx)
	auto, threshold = true, voiceid.DefaultThreshold
	if s.VoiceAuto != nil {
		auto = *s.VoiceAuto
	}
	if s.VoiceThreshold != nil && *s.VoiceThreshold > 0 {
		threshold = *s.VoiceThreshold
	}
	return auto, threshold
}

// AfterTranscription runs when a job finishes: the voice scan first (so
// known speakers are named), then the automatic summary.
func (h *Handler) AfterTranscription(jobID string) {
	if auto, _ := h.voiceSettings(context.Background()); auto && h.voicesReady() {
		h.queueVoiceScan(voiceTask{JobID: jobID, Reason: "new recording", First: true, Then: func() { h.AutoSummarize(jobID) }})
		return
	}
	h.AutoSummarize(jobID)
}

// speakerTurns reads a stored transcript's speaker turns, split at word
// speaker changes.
func speakerTurns(transcript string) []adapters.VoiceTurn {
	var r interfaces.TranscriptResult
	if json.Unmarshal([]byte(transcript), &r) != nil {
		return nil
	}
	speakers.Apply(&r)
	var out []adapters.VoiceTurn
	for _, s := range r.Segments {
		if s.Speaker != nil && *s.Speaker != "" && s.End > s.Start {
			out = append(out, adapters.VoiceTurn{Speaker: *s.Speaker, Start: s.Start, End: s.End})
		}
	}
	return out
}

func toPrints(stored []models.VoicePrint) []voiceid.Print {
	out := make([]voiceid.Print, 0, len(stored))
	for _, p := range stored {
		vp := voiceid.Print{Speaker: p.Speaker, Vector: p.Vector, Seconds: p.Seconds}
		var chunks []adapters.VoiceChunk
		if p.Chunks != "" && json.Unmarshal([]byte(p.Chunks), &chunks) == nil {
			for _, c := range chunks {
				vp.Chunks = append(vp.Chunks, voiceid.Chunk{Start: c.Start, End: c.End, Score: c.Score})
			}
		}
		out = append(out, vp)
	}
	return out
}

// scanVoices makes a recording's voice prints, flags it for a redo when its
// speakers look wrong, and names speakers that match a known voice.
func (h *Handler) scanVoices(ctx context.Context, jobID string) (err error) {
	start := time.Now()
	setStatus := func(status, issues string) {
		if e := h.voiceRepo.SetVoiceStatus(context.Background(), jobID, status, issues); e != nil {
			logger.Warn("Could not save voice scan status", "job_id", jobID, "error", e)
		}
	}
	job, err := h.jobRepo.FindByID(ctx, jobID)
	if err != nil || job == nil {
		return fmt.Errorf("recording not found")
	}
	if job.IsMultiTrack || job.Transcript == nil {
		setStatus(models.VoiceNone, "")
		return nil
	}
	turns := speakerTurns(*job.Transcript)
	if len(turns) == 0 {
		setStatus(models.VoiceNone, "")
		return nil
	}
	if _, statErr := os.Stat(job.AudioPath); statErr != nil {
		setStatus(models.VoiceNone, "audio file missing")
		return nil
	}
	setStatus(models.VoiceRunning, "")

	release, err := gpu.Acquire(ctx)
	if err != nil {
		setStatus("", "")
		return err
	}
	var logW io.Writer
	if f, e := os.OpenFile(h.jobLogPath(jobID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); e == nil {
		defer f.Close()
		fmt.Fprintf(f, "\n=== Voice scan %s ===\n", time.Now().Format(time.RFC3339))
		logW = f
	}
	sctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	res, err := h.voicePrinter.VoicePrints(sctx, job.AudioPath, turns, logW)
	cancel()
	release()
	if err != nil {
		logger.Warn("Voice scan failed", "job_id", jobID, "error", err)
		msg := err.Error()
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		setStatus(models.VoiceFailed, msg)
		return err
	}

	var stored []models.VoicePrint
	for sp, p := range res.Speakers {
		chunks, _ := json.Marshal(p.Chunks)
		stored = append(stored, models.VoicePrint{Speaker: sp, Vector: p.Embedding, Seconds: p.Seconds, Chunks: string(chunks), Model: res.Model})
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].Speaker < stored[j].Speaker })
	if err := h.voiceRepo.SavePrints(ctx, jobID, stored); err != nil {
		setStatus(models.VoiceFailed, "could not save voice prints: "+err.Error())
		return err
	}

	problems := voiceid.Assess(toPrints(stored))
	issues := ""
	if len(problems) > 0 {
		b, _ := json.Marshal(problems)
		issues = string(b)
	}
	if err := h.voiceRepo.SetFlag(ctx, jobID, models.FlagRedo, len(problems) > 0); err != nil {
		logger.Warn("Could not update redo flag", "job_id", jobID, "error", err)
	}
	setStatus(models.VoiceDone, issues)

	// Profiles built from this recording use the new prints
	if samples, err := h.voiceRepo.SampleSpeakers(ctx, jobID); err == nil {
		seen := map[string]bool{}
		for _, pid := range samples {
			if !seen[pid] {
				seen[pid] = true
				h.refreshProfile(ctx, pid)
			}
		}
	}
	named, _ := h.applyVoiceMatches(ctx, jobID)
	logger.Info("Voice scan done", "job_id", jobID, "speakers", len(stored), "named", named,
		"problems", len(problems), "duration", time.Since(start).Round(time.Second))
	if len(problems) > 0 || named > 0 {
		h.SyncRecordingAsync(jobID)
	}
	return nil
}

// applyVoiceMatches names a recording's speakers from voice profiles.
// Names given by hand are never changed. Returns how many speakers carry
// a voice name afterwards.
func (h *Handler) applyVoiceMatches(ctx context.Context, jobID string) (int, error) {
	stored, err := h.voiceRepo.ListPrints(ctx, jobID)
	if err != nil || len(stored) == 0 {
		return 0, err
	}
	profiles, err := h.voiceRepo.ListProfiles(ctx)
	if err != nil {
		return 0, err
	}
	var ps []voiceid.Profile
	for _, p := range profiles {
		if len(p.Vector) > 0 {
			ps = append(ps, voiceid.Profile{ID: p.ID, Name: p.Name, Vector: p.Vector})
		}
	}
	mappings, err := h.speakerMappingRepo.ListByJob(ctx, jobID)
	if err != nil {
		return 0, err
	}
	skip, taken := map[string]bool{}, map[string]bool{}
	var keep []models.SpeakerMapping
	oldVoice := map[string]models.SpeakerMapping{}
	generic := map[string]models.SpeakerMapping{} // "Speaker 2" given by hand: a voice name may replace it
	for _, m := range mappings {
		if m.Source == models.MappingVoice {
			oldVoice[m.OriginalSpeaker] = m
			continue
		}
		if !voiceid.Enrollable(m.CustomName) || m.CustomName == m.OriginalSpeaker {
			generic[m.OriginalSpeaker] = m
			continue
		}
		skip[m.OriginalSpeaker] = true
		taken[strings.ToLower(m.CustomName)] = true
		keep = append(keep, m)
	}
	_, threshold := h.voiceSettings(ctx)
	matches := voiceid.Assign(toPrints(stored), ps, threshold, skip, taken)
	matched := map[string]bool{}
	for _, m := range matches {
		matched[m.Speaker] = true
	}
	for sp, m := range generic {
		if matched[sp] {
			oldVoice[sp] = models.SpeakerMapping{} // replaced: counts as a change
			continue
		}
		keep = append(keep, m)
	}

	changed := len(matches) != len(oldVoice)
	for _, m := range matches {
		pid := m.ProfileID
		if o, ok := oldVoice[m.Speaker]; !ok || o.CustomName != m.Name || o.VoiceProfileID == nil || *o.VoiceProfileID != pid ||
			absf(o.Score-m.Score) > 0.0005 {
			changed = true
		}
		keep = append(keep, models.SpeakerMapping{TranscriptionJobID: jobID, OriginalSpeaker: m.Speaker, CustomName: m.Name,
			Source: models.MappingVoice, VoiceProfileID: &pid, Score: roundf(m.Score)})
	}
	if !changed {
		return len(matches), nil
	}
	for i := range keep {
		keep[i].ID = 0
		keep[i].TranscriptionJob = models.TranscriptionJob{}
	}
	return len(matches), h.speakerMappingRepo.UpdateMappings(ctx, jobID, keep)
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func roundf(x float64) float64 { return float64(int(x*1000+0.5)) / 1000 }

// refreshProfile rebuilds a profile's voice from its samples. A profile
// with no samples left is removed.
func (h *Handler) refreshProfile(ctx context.Context, profileID string) {
	p, err := h.voiceRepo.GetProfile(ctx, profileID)
	if err != nil {
		return
	}
	samples, err := h.voiceRepo.ListSamples(ctx, profileID)
	if err != nil {
		return
	}
	var vs []voiceid.Sample
	var secs float64
	byJob := map[string][]models.VoicePrint{}
	for _, s := range samples {
		prints, ok := byJob[s.TranscriptionJobID]
		if !ok {
			prints, _ = h.voiceRepo.ListPrints(ctx, s.TranscriptionJobID)
			byJob[s.TranscriptionJobID] = prints
		}
		for _, pr := range prints {
			if pr.Speaker == s.Speaker && len(pr.Vector) > 0 {
				vs = append(vs, voiceid.Sample{Vector: pr.Vector, Seconds: pr.Seconds})
				secs += pr.Seconds
			}
		}
	}
	if len(samples) == 0 {
		if err := h.voiceRepo.DeleteProfile(ctx, profileID); err == nil {
			logger.Info("Removed voice profile with no samples left", "name", p.Name)
		}
		return
	}
	p.Vector = voiceid.Centroid(vs)
	p.Samples = len(vs)
	p.Seconds = roundf(secs)
	if err := h.voiceRepo.SaveProfile(ctx, p); err != nil {
		logger.Warn("Could not save voice profile", "name", p.Name, "error", err)
	}
}

// enrollFromMappings learns voices from the names given by hand in one
// recording, then re-applies all profiles to the library in the
// background.
func (h *Handler) enrollFromMappings(ctx context.Context, jobID string, mappings []models.SpeakerMapping) {
	if h.voiceRepo == nil {
		return
	}
	prints, err := h.voiceRepo.ListPrints(ctx, jobID)
	if err != nil {
		return
	}
	bySpeaker := map[string]models.VoicePrint{}
	for _, p := range prints {
		bySpeaker[p.Speaker] = p
	}
	touched := map[string]bool{}
	named := map[string]bool{}
	for _, m := range mappings {
		if m.Source == models.MappingVoice || !voiceid.Enrollable(m.CustomName) {
			continue
		}
		pr, ok := bySpeaker[m.OriginalSpeaker]
		if !ok || len(pr.Vector) == 0 || pr.Seconds < voiceid.MinEnrollSeconds {
			continue
		}
		prof, err := h.voiceRepo.ProfileByName(ctx, m.CustomName)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			prof, err = h.voiceRepo.CreateProfile(ctx, m.CustomName)
			if err == nil {
				logger.Info("New voice profile", "name", prof.Name)
			}
		}
		if err != nil || prof == nil {
			continue
		}
		prev, err := h.voiceRepo.SetSample(ctx, prof.ID, jobID, m.OriginalSpeaker)
		if err != nil {
			continue
		}
		named[m.OriginalSpeaker] = true
		touched[prof.ID] = true
		if prev != "" {
			touched[prev] = true
		}
	}
	// Speakers no longer named by hand stop being samples
	if samples, err := h.voiceRepo.SampleSpeakers(ctx, jobID); err == nil {
		for sp := range samples {
			if !named[sp] {
				if pid, err := h.voiceRepo.RemoveSample(ctx, jobID, sp); err == nil && pid != "" {
					touched[pid] = true
				}
			}
		}
	}
	if len(touched) == 0 {
		return
	}
	for pid := range touched {
		h.refreshProfile(ctx, pid)
	}
	h.rematchLibrary()
}

// rematchLibrary re-applies voice profiles to every scanned recording in
// the background. Calls while it runs make it run once more at the end.
func (h *Handler) rematchLibrary() {
	r := h.voices
	r.matchMu.Lock()
	if r.matching {
		r.matchAgain = true
		r.matchMu.Unlock()
		return
	}
	r.matching = true
	r.matchMu.Unlock()
	go func() {
		for {
			ctx := context.Background()
			n, errText := 0, ""
			ids, err := h.voiceRepo.JobsWithPrints(ctx)
			if err != nil {
				errText = err.Error()
			}
			for _, id := range ids {
				if c, err := h.applyVoiceMatches(ctx, id); err == nil {
					n += c
				}
			}
			logger.Info("Voice profiles applied to library", "recordings", len(ids), "named_speakers", n)
			r.matchMu.Lock()
			r.lastMatched, r.lastMatchErr = n, errText
			if !r.matchAgain {
				r.matching = false
				r.matchMu.Unlock()
				return
			}
			r.matchAgain = false
			r.matchMu.Unlock()
		}
	}()
}

// SplitStoredSegments rewrites stored transcripts so each segment has one
// speaker (cut where word speakers change). Words, and so notes, are not
// touched. Safe to run on every start: unchanged transcripts are skipped.
func (h *Handler) SplitStoredSegments(ctx context.Context) {
	jobs, err := h.jobRepo.ListStorageInfo(ctx)
	if err != nil {
		return
	}
	start := time.Now()
	changed := 0
	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		job, err := h.jobRepo.FindByID(ctx, j.ID)
		if err != nil || job == nil || job.Transcript == nil {
			continue
		}
		out, ok, err := speakers.RewriteJSON(*job.Transcript)
		if err != nil || !ok {
			continue
		}
		if err := h.jobRepo.UpdateTranscript(ctx, j.ID, out); err != nil {
			logger.Warn("Could not save split transcript", "job_id", j.ID, "error", err)
			continue
		}
		changed++
		h.SyncRecording(ctx, j.ID)
	}
	if changed > 0 {
		logger.Info("Split transcript segments at speaker changes", "recordings", changed, "duration", time.Since(start).Round(time.Second))
	}
}
