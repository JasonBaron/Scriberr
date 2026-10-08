package recordings

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"scriberr/internal/models"
)

// Metadata is what metadata.json holds for a recording.
type Metadata struct {
	ID               string     `json:"id"`
	Title            string     `json:"title,omitempty"`
	Status           string     `json:"status"`
	Brief            string     `json:"brief,omitempty"`
	Tags             []string   `json:"tags,omitempty"`
	RecordedAt       *time.Time `json:"recorded_at,omitempty"`
	RecordedAtSource string     `json:"recorded_at_source,omitempty"`
	UploadedAt       time.Time  `json:"uploaded_at"`
	OriginalFilename string     `json:"original_filename,omitempty"`
	AudioFile        string     `json:"audio_file,omitempty"`
	SHA256           string     `json:"sha256,omitempty"`
	FileSize         int64      `json:"file_size,omitempty"`
	Summaries        []string   `json:"summaries,omitempty"`
	Note             string     `json:"note"`
}

// SummaryNamer is the template name for a summary's template ID.
type SummaryNamer func(templateID *string) string

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		return "summary"
	}
	return s
}

// SummaryFileName names a summary file so they sort by time and stay
// unique: 2026-10-08_1550_default_1a2b3c4d.md
func SummaryFileName(s models.Summary, templateName string, loc *time.Location) string {
	id := s.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("%s_%s_%s.md", s.CreatedAt.In(loc).Format("2006-01-02_1504"), slug(templateName), id)
}

// Mirror builds the files that copy a recording's database state:
// metadata.json, transcript.json and summaries/*.md.
func Mirror(job *models.TranscriptionJob, summaries []models.Summary, name SummaryNamer, loc *time.Location) (top map[string][]byte, summaryFiles map[string][]byte, err error) {
	top = map[string][]byte{}
	summaryFiles = map[string][]byte{}

	for _, s := range summaries {
		tname := name(s.TemplateID)
		fname := SummaryFileName(s, tname, loc)
		header := fmt.Sprintf("<!-- %s | %s | %s -->\n\n", tname, s.Model, s.CreatedAt.In(loc).Format("2006-01-02 15:04 MST"))
		summaryFiles[fname] = []byte(header + strings.TrimSpace(s.Content) + "\n")
	}

	meta := Metadata{
		ID:               job.ID,
		Status:           string(job.Status),
		Tags:             job.Tags,
		RecordedAt:       job.RecordedAt,
		RecordedAtSource: job.RecordedAtSource,
		UploadedAt:       job.CreatedAt,
		OriginalFilename: job.OriginalFilename,
		AudioFile:        filepath.Base(job.AudioPath),
		SHA256:           job.FileHash,
		FileSize:         job.FileSize,
		Note:             "Copy of Scriberr's database, rewritten when the recording changes. Edits here are not read back.",
	}
	if job.Title != nil {
		meta.Title = *job.Title
	}
	if job.SummaryBrief != nil {
		meta.Brief = *job.SummaryBrief
	}
	for name := range summaryFiles {
		meta.Summaries = append(meta.Summaries, "summaries/"+name)
	}
	sortStrings(meta.Summaries)
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	top["metadata.json"] = append(b, '\n')

	if job.Transcript != nil && strings.TrimSpace(*job.Transcript) != "" {
		var v interface{}
		if json.Unmarshal([]byte(*job.Transcript), &v) == nil {
			if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
				top["transcript.json"] = append(pretty, '\n')
			}
		}
	}
	return top, summaryFiles, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
