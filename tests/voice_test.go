package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"scriberr/internal/models"
	"scriberr/internal/repository"
	"scriberr/internal/transcription/adapters"

	"github.com/stretchr/testify/assert"
)

// fakePrinter returns fixed voice prints per audio file and speaker.
type fakePrinter struct {
	mu     sync.Mutex
	prints map[string]map[string]adapters.SpeakerPrint // audio path -> speaker -> print
	calls  int
}

func (f *fakePrinter) VoicePrints(ctx context.Context, audioPath string, turns []adapters.VoiceTurn, logW io.Writer) (*adapters.VoicePrintResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	res := &adapters.VoicePrintResult{Model: "fake", Dim: 3, Speakers: map[string]adapters.SpeakerPrint{}}
	seen := map[string]bool{}
	for _, t := range turns {
		seen[t.Speaker] = true
	}
	for sp, p := range f.prints[audioPath] {
		if seen[sp] {
			res.Speakers[sp] = p
		}
	}
	return res, nil
}

func goodChunks() []adapters.VoiceChunk {
	return []adapters.VoiceChunk{{Start: 0, End: 8, Score: 0.8}, {Start: 8, End: 16, Score: 0.75}, {Start: 16, End: 24, Score: 0.7}, {Start: 24, End: 32, Score: 0.8}}
}

func transcriptFor(speakers ...string) string {
	type seg struct {
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		Text    string  `json:"text"`
		Speaker string  `json:"speaker"`
	}
	type word struct {
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		Word    string  `json:"word"`
		Speaker string  `json:"speaker"`
	}
	var segs []seg
	var words []word
	for i, sp := range speakers {
		st := float64(i * 30)
		segs = append(segs, seg{st, st + 30, "hello there", sp})
		words = append(words, word{st, st + 1, "hello", sp}, word{st + 1, st + 2, "there", sp})
	}
	b, _ := json.Marshal(map[string]interface{}{"text": "x", "segments": segs, "word_segments": words})
	return string(b)
}

func (suite *APIHandlerTestSuite) newVoiceJob(name, audio string, transcript string) *models.TranscriptionJob {
	t := suite.T()
	job := suite.helper.CreateTestTranscriptionJob(t, name)
	job.Status = models.StatusCompleted
	job.Diarization = true
	job.AudioPath = audio
	job.Transcript = &transcript
	assert.NoError(t, suite.helper.DB.Save(job).Error)
	return job
}

func (suite *APIHandlerTestSuite) waitVoice(jobID string) models.TranscriptionJob {
	var job models.TranscriptionJob
	for i := 0; i < 100; i++ {
		suite.helper.DB.First(&job, "id = ?", jobID)
		if job.VoiceStatus == models.VoiceDone || job.VoiceStatus == models.VoiceFailed || job.VoiceStatus == models.VoiceNone {
			return job
		}
		time.Sleep(30 * time.Millisecond)
	}
	suite.T().Fatalf("voice scan did not finish: %q", job.VoiceStatus)
	return job
}

func (suite *APIHandlerTestSuite) mappingsOf(jobID string) map[string]models.SpeakerMapping {
	var ms []models.SpeakerMapping
	suite.helper.DB.Where("transcription_job_id = ?", jobID).Find(&ms)
	out := map[string]models.SpeakerMapping{}
	for _, m := range ms {
		out[m.OriginalSpeaker] = m
	}
	return out
}

func (suite *APIHandlerTestSuite) eventually(cond func() bool, what string) {
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	suite.T().Fatalf("timed out waiting for %s", what)
}

func (suite *APIHandlerTestSuite) TestVoiceID() {
	t := suite.T()
	db := suite.helper.DB
	for _, tbl := range []string{"voice_prints", "voice_profiles", "voice_samples", "speaker_mappings", "summary_settings"} {
		db.Exec("DELETE FROM " + tbl)
	}
	dir := t.TempDir()
	audio := func(n string) string {
		p := filepath.Join(dir, n)
		assert.NoError(t, os.WriteFile(p, []byte("x"), 0644))
		return p
	}
	a, b := audio("a.wav"), audio("b.wav")
	printer := &fakePrinter{prints: map[string]map[string]adapters.SpeakerPrint{
		a: {
			"S0": {Embedding: []float64{1, 0, 0}, Seconds: 60, Chunks: goodChunks()},
			"S1": {Embedding: []float64{0, 1, 0}, Seconds: 60, Chunks: goodChunks()},
		},
		b: {
			"X0": {Embedding: []float64{0.9, 0.3, 0}, Seconds: 45, Chunks: goodChunks()},
			// Two voices under one label: half the audio scores low
			"X1": {Embedding: []float64{0, 0, 1}, Seconds: 45, Chunks: []adapters.VoiceChunk{
				{Start: 0, End: 8, Score: 0.8}, {Start: 8, End: 16, Score: 0.1}, {Start: 16, End: 24, Score: 0.75}, {Start: 24, End: 32, Score: 0.2}}},
		},
	}}
	suite.handler.SetVoices(repository.NewVoiceRepository(db), printer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	suite.handler.StartVoiceWorker(ctx)

	w := suite.makeAuthenticatedRequest("GET", "/api/v1/voices", nil, false)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"available":true`)
	assert.Contains(t, w.Body.String(), `"auto":true`)

	// Scan the first recording: prints saved, nothing named yet
	jobA := suite.newVoiceJob("a", a, transcriptFor("S0", "S1"))
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/transcription/"+jobA.ID+"/voices/scan", nil, false)
	assert.Equal(t, 200, w.Code)
	got := suite.waitVoice(jobA.ID)
	assert.Equal(t, models.VoiceDone, got.VoiceStatus)
	assert.NotContains(t, []string(got.Flags), models.FlagRedo)
	var n int64
	db.Model(&models.VoicePrint{}).Where("transcription_job_id = ?", jobA.ID).Count(&n)
	assert.Equal(t, int64(2), n)

	// Naming a speaker by hand teaches a voice; generic names do not
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/transcription/"+jobA.ID+"/speakers", map[string]interface{}{
		"mappings": []map[string]string{{"original_speaker": "S0", "custom_name": "Jason"}, {"original_speaker": "S1", "custom_name": "Speaker 2"}},
	}, false)
	assert.Equal(t, 200, w.Code)
	suite.eventually(func() bool {
		var p models.VoiceProfile
		return db.First(&p, "name = ?", "Jason").Error == nil && len(p.Vector) == 3
	}, "Jason profile")
	db.Model(&models.VoiceProfile{}).Count(&n)
	assert.Equal(t, int64(1), n, "Speaker 2 is not a voice")

	// A new recording: Jason is named by voice, and the mixed label flags a redo
	jobB := suite.newVoiceJob("b", b, transcriptFor("X0", "X1"))
	suite.handler.AfterTranscription(jobB.ID)
	got = suite.waitVoice(jobB.ID)
	assert.Contains(t, []string(got.Flags), models.FlagRedo)
	assert.Contains(t, got.VoiceIssues, "X1 sounds like more than one person")
	m := suite.mappingsOf(jobB.ID)
	assert.Equal(t, "Jason", m["X0"].CustomName)
	assert.Equal(t, models.MappingVoice, m["X0"].Source)
	assert.Greater(t, m["X0"].Score, 0.9)
	_, named := m["X1"]
	assert.False(t, named, "an unknown voice stays unnamed")

	w = suite.makeAuthenticatedRequest("GET", "/api/v1/transcription/"+jobB.ID+"/voices", nil, false)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"name":"Jason"`)
	assert.Contains(t, w.Body.String(), `"kind":"mixed"`)
	w = suite.makeAuthenticatedRequest("GET", "/api/v1/transcription/"+jobB.ID+"/speakers", nil, false)
	assert.Contains(t, w.Body.String(), `"source":"voice"`)

	// Saving the dialog without changes keeps it a voice name (not a new sample)
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/transcription/"+jobB.ID+"/speakers", map[string]interface{}{
		"mappings": []map[string]string{{"original_speaker": "X0", "custom_name": "Jason"}},
	}, false)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, models.MappingVoice, suite.mappingsOf(jobB.ID)["X0"].Source)
	time.Sleep(100 * time.Millisecond)
	db.Model(&models.VoiceSample{}).Count(&n)
	assert.Equal(t, int64(1), n)

	// Renaming the voice renames its speakers everywhere
	var prof models.VoiceProfile
	assert.NoError(t, db.First(&prof, "name = ?", "Jason").Error)
	w = suite.makeAuthenticatedRequest("PATCH", "/api/v1/voices/"+prof.ID, map[string]string{"name": "Jason B"}, false)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "Jason B", suite.mappingsOf(jobB.ID)["X0"].CustomName)
	assert.Equal(t, "Jason B", suite.mappingsOf(jobA.ID)["S0"].CustomName)

	// A stricter threshold than the match removes the voice name
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/voices/settings", map[string]interface{}{"threshold": 0.995}, false)
	assert.Equal(t, 400, w.Code)
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/voices/settings", map[string]interface{}{"threshold": 0.95}, false)
	assert.Equal(t, 200, w.Code)
	suite.eventually(func() bool { _, ok := suite.mappingsOf(jobB.ID)["X0"]; return !ok }, "voice name removed at 0.95")
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/voices/settings", map[string]interface{}{"threshold": 0.6}, false)
	suite.eventually(func() bool { return suite.mappingsOf(jobB.ID)["X0"].CustomName == "Jason B" }, "voice name back at 0.6")

	// Deleting the voice removes names it gave; names given by hand stay
	w = suite.makeAuthenticatedRequest("DELETE", "/api/v1/voices/"+prof.ID, nil, false)
	assert.Equal(t, 200, w.Code)
	_, still := suite.mappingsOf(jobB.ID)["X0"]
	assert.False(t, still)
	assert.Equal(t, "Jason B", suite.mappingsOf(jobA.ID)["S0"].CustomName)

	// Library scan skips recordings already scanned
	w = suite.makeAuthenticatedRequest("POST", "/api/v1/voices/scan", map[string]interface{}{}, false)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"queued":0`)
	assert.Equal(t, 2, printer.calls)
}

func (suite *APIHandlerTestSuite) TestSplitStoredSegments() {
	t := suite.T()
	raw := `{"text":"hi yes","segments":[{"start":0,"end":4,"text":"hi yes","speaker":"A"}],` +
		`"word_segments":[{"start":0,"end":1,"word":"hi","speaker":"A"},{"start":2,"end":3,"word":"yes","speaker":"B"}]}`
	job := suite.newVoiceJob("split", "nowhere.wav", raw)
	suite.handler.SplitStoredSegments(context.Background())
	var got models.TranscriptionJob
	assert.NoError(t, suite.helper.DB.First(&got, "id = ?", job.ID).Error)
	assert.Equal(t, 2, strings.Count(*got.Transcript, `"speaker":"B"`), fmt.Sprintf("segment and word for B: %s", *got.Transcript))
	before := *got.Transcript
	suite.handler.SplitStoredSegments(context.Background())
	assert.NoError(t, suite.helper.DB.First(&got, "id = ?", job.ID).Error)
	assert.Equal(t, before, *got.Transcript, "second run changes nothing")
}
