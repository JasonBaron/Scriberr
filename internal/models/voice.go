package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Floats is a float list stored as JSON text.
type Floats []float64

// Value implements driver.Valuer.
func (f Floats) Value() (driver.Value, error) {
	if len(f) == 0 {
		return nil, nil
	}
	b, err := json.Marshal([]float64(f))
	return string(b), err
}

// Scan implements sql.Scanner.
func (f *Floats) Scan(src interface{}) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*f = nil
		return nil
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("Floats: unsupported type %T", src)
	}
	if len(raw) == 0 {
		*f = nil
		return nil
	}
	return json.Unmarshal(raw, (*[]float64)(f))
}

// VoicePrint is one speaker's voice in one recording, from the PyAnnote
// speaker embedding model.
type VoicePrint struct {
	ID                 uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	TranscriptionJobID string    `json:"transcription_job_id" gorm:"type:varchar(36);not null;index"`
	Speaker            string    `json:"speaker" gorm:"type:varchar(50);not null"`
	Vector             Floats    `json:"-" gorm:"type:text"`
	Seconds            float64   `json:"seconds"`
	Chunks             string    `json:"chunks,omitempty" gorm:"type:text"` // JSON [[start, end, score], ...]
	Model              string    `json:"model" gorm:"type:varchar(120)"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime"`
}

// VoiceProfile is a named voice built from the prints of speakers named by
// hand.
type VoiceProfile struct {
	ID      string  `json:"id" gorm:"primaryKey;type:varchar(36)"`
	Name    string  `json:"name" gorm:"type:varchar(100);not null;uniqueIndex"`
	Vector  Floats  `json:"-" gorm:"type:text"`
	Samples int     `json:"samples"`
	Seconds float64 `json:"seconds"`
	// SeedVector is a voice brought in from elsewhere (another server), kept
	// as one more sample so the first name given here refines the voice
	// instead of replacing it.
	SeedVector  Floats    `json:"-" gorm:"type:text"`
	SeedSeconds float64   `json:"seed_seconds,omitempty"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// VoiceSample links a profile to a speaker in a recording.
type VoiceSample struct {
	ID                 uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	ProfileID          string    `json:"profile_id" gorm:"type:varchar(36);not null;index"`
	TranscriptionJobID string    `json:"transcription_job_id" gorm:"type:varchar(36);not null;index"`
	Speaker            string    `json:"speaker" gorm:"type:varchar(50);not null"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime"`
}

// Voice scan states on a recording.
const (
	VoiceDone    = "done"    // prints saved
	VoiceNone    = "none"    // nothing to scan (no speakers or no audio)
	VoiceFailed  = "failed"  // the scan failed; see VoiceIssues
	VoiceQueued  = "queued"  // waiting
	VoiceRunning = "running" // in progress
)

// Speaker mapping sources.
const (
	MappingUser  = ""      // named by hand
	MappingVoice = "voice" // named by voice match
)

// FlagRedo marks recordings whose speakers look wrong and should be
// transcribed again.
const FlagRedo = "redo"
