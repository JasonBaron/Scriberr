package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SummaryTemplate represents a saved summarization prompt/template
type SummaryTemplate struct {
	ID                 string  `json:"id" gorm:"primaryKey;type:varchar(36)"`
	Name               string  `json:"name" gorm:"type:varchar(255);not null"`
	Description        *string `json:"description,omitempty" gorm:"type:text"`
	Model              string  `json:"model" gorm:"type:varchar(255);not null;default:''"`
	Prompt             string  `json:"prompt" gorm:"type:text;not null"`
	IncludeSpeakerInfo bool    `json:"include_speaker_info" gorm:"default:false"`
	// Reasoning lets reasoning models (qwen3, deepseek-r1) think before
	// answering. Off by default: summaries rarely need it and it is slower.
	Reasoning bool `json:"reasoning" gorm:"default:false"`
	// IsDefault marks the template preselected in the Summarize dialog. At
	// most one template is the default.
	IsDefault bool `json:"is_default" gorm:"default:false"`
	// AutoTags runs this template automatically on recordings that have any
	// of these tags (after the default summary has tagged them).
	AutoTags StringList `json:"auto_tags,omitempty" gorm:"type:text"`
	// Enabled templates run automatically and appear in the Summarize
	// dialog. Disabled ones are kept but never run on their own.
	Enabled *bool `json:"enabled" gorm:"default:true"`
	// BuiltinKey links a template to one shipped with Scriberr, so it can
	// be reset to the shipped version. Empty for templates made by hand.
	BuiltinKey string `json:"builtin_key,omitempty" gorm:"type:varchar(64);index"`
	// Customized reports that a built-in template differs from the shipped
	// version. Computed, not stored.
	Customized bool      `json:"customized" gorm:"-"`
	CreatedAt  time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt  time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// IsEnabled treats a missing value as enabled.
func (st *SummaryTemplate) IsEnabled() bool { return st.Enabled == nil || *st.Enabled }

func (st *SummaryTemplate) BeforeCreate(tx *gorm.DB) error {
	if st.ID == "" {
		st.ID = uuid.New().String()
	}
	return nil
}

// SummarySetting stores global settings for summarization (single row)
type SummarySetting struct {
	ID           uint   `json:"id" gorm:"primaryKey"`
	DefaultModel string `json:"default_model" gorm:"type:varchar(255);not null;default:''"`
	// AutoSummarize runs the default template on every completed transcription.
	AutoSummarize bool `json:"auto_summarize" gorm:"default:false"`
	// OwnerName is how the person who records is labeled in transcripts.
	// Templates refer to them as {me}.
	OwnerName string `json:"owner_name" gorm:"type:varchar(255);not null;default:''"`
	// RedactPII removes dates of birth, ID, account and card numbers, phone
	// numbers, emails and street addresses from saved summaries.
	RedactPII *bool `json:"redact_pii" gorm:"default:true"`
	// Tag vocabulary. Empty text means the built-in default.
	TagStrict   *bool  `json:"tag_strict" gorm:"default:true"`
	TagTypes    string `json:"tag_types" gorm:"type:text"`
	TagTopics   string `json:"tag_topics" gorm:"type:text"`
	TagSynonyms string `json:"tag_synonyms" gorm:"type:text"`
	// TagNameHints sets the type from words in the recording's own name.
	TagNameHints string `json:"tag_name_hints" gorm:"type:text"`
	// TagKeywords is how many free-form keywords go next to the type and
	// topics (0 to 5). Nil means the default, 2.
	TagKeywords *int      `json:"tag_keywords"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// Summary stores a generated summary linked to a transcription
type Summary struct {
	ID              string    `json:"id" gorm:"primaryKey;type:varchar(36)"`
	TranscriptionID string    `json:"transcription_id" gorm:"type:varchar(36);index;not null"`
	TemplateID      *string   `json:"template_id,omitempty" gorm:"type:varchar(36)"`
	Model           string    `json:"model" gorm:"type:varchar(255);not null"`
	Content         string    `json:"content" gorm:"type:text;not null"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// Relationships
	Transcription TranscriptionJob `json:"transcription,omitempty" gorm:"foreignKey:TranscriptionID;constraint:OnDelete:CASCADE"`
}

// BeforeCreate ensures Summary has a UUID primary key
func (s *Summary) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

// JobSuggestion is what the model proposed for a job after a summary.
type JobSuggestion struct {
	Title string
	Brief string
	Tags  StringList
	// Flags are always replaced, even when the tags were edited by hand.
	Flags StringList
	// OverwriteEditedTags replaces tags the user edited by hand and clears
	// the edited mark (retagging with "include hand-edited").
	OverwriteEditedTags bool
}
