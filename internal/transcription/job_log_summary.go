package transcription

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scriberr/internal/models"
	"scriberr/internal/transcription/interfaces"
)

// Helpers that turn job state into short, human-readable job log values.
// None of them include secrets or transcript text.

func strOr(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

// audioSummary returns e.g. "meeting.m4a (61.9 MB)".
func audioSummary(path string) string {
	if path == "" {
		return ""
	}
	name := filepath.Base(path)
	if st, err := os.Stat(path); err == nil {
		return fmt.Sprintf("%s (%.1f MB)", name, float64(st.Size())/(1024*1024))
	}
	return name
}

// hfTokenStatus reports where the Hugging Face token comes from, never its value.
func hfTokenStatus(p models.WhisperXParams) string {
	switch {
	case p.HfToken != nil && *p.HfToken != "":
		return "present (job parameters)"
	case os.Getenv("HF_TOKEN") != "":
		return "present (env)"
	case p.Diarize:
		return "MISSING (pyannote diarization will fail)"
	default:
		return "not set (not needed)"
	}
}

// profileSummary returns e.g.
// "whisper large-v3 | cuda float16 | batch 4 | en | VAD pyannote 0.50/0.363 | diarize pyannote (2-2 speakers)".
func profileSummary(p models.WhisperXParams) string {
	family := p.ModelFamily
	if family == "" {
		family = FamilyWhisper
	}
	parts := []string{strings.TrimSpace(family + " " + p.Model)}
	parts = append(parts, strings.TrimSpace(p.Device+" "+p.ComputeType))
	if p.BatchSize > 0 {
		parts = append(parts, fmt.Sprintf("batch %d", p.BatchSize))
	}
	parts = append(parts, strOr(p.Language, "language auto"))
	if family == FamilyWhisper && p.VadMethod != "" {
		parts = append(parts, fmt.Sprintf("VAD %s %.2f/%.3f", p.VadMethod, p.VadOnset, p.VadOffset))
	}
	if p.Diarize {
		d := "diarize " + p.DiarizeModel
		if p.MinSpeakers != nil || p.MaxSpeakers != nil {
			lo, hi := "?", "?"
			if p.MinSpeakers != nil {
				lo = fmt.Sprint(*p.MinSpeakers)
			}
			if p.MaxSpeakers != nil {
				hi = fmt.Sprint(*p.MaxSpeakers)
			}
			d += fmt.Sprintf(" (%s-%s speakers)", lo, hi)
		}
		parts = append(parts, d)
	} else {
		parts = append(parts, "no diarization")
	}
	return strings.Join(parts, " | ")
}

// audioNote returns e.g. "300.0 s, converted to 16000 Hz mono".
func audioNote(original, prepared interfaces.AudioInput, prepErr error) string {
	s := fmt.Sprintf("%.1f s", original.Duration.Seconds())
	if original.Format != "" {
		s += ", " + original.Format
	}
	switch {
	case prepErr != nil:
		s += fmt.Sprintf(", preprocessing failed so using original: %v", prepErr)
	case prepared.TempFilePath != "" && prepared.TempFilePath != original.FilePath:
		ch := "mono"
		if prepared.Channels != 1 {
			ch = fmt.Sprintf("%d ch", prepared.Channels)
		}
		s += fmt.Sprintf(", converted to %d Hz %s", prepared.SampleRate, ch)
	default:
		s += ", no conversion needed"
	}
	return s
}

// transcriptNote returns e.g. "62 segments, 812 words, 2 speakers".
func transcriptNote(r *interfaces.TranscriptResult) string {
	if r == nil {
		return "no result"
	}
	speakers := map[string]bool{}
	words := len(r.WordSegments)
	for _, s := range r.Segments {
		if s.Speaker != nil && *s.Speaker != "" {
			speakers[*s.Speaker] = true
		}
		if len(r.WordSegments) == 0 {
			words += len(strings.Fields(s.Text))
		}
	}
	note := fmt.Sprintf("%d segments, %d words", len(r.Segments), words)
	if len(speakers) > 0 {
		note += fmt.Sprintf(", %d speakers", len(speakers))
	}
	return note
}

// diarizationNote returns e.g. "2 speakers, 1033 segments".
func diarizationNote(r *interfaces.DiarizationResult) string {
	if r == nil {
		return "no result"
	}
	n := r.SpeakerCount
	if n == 0 {
		n = len(r.Speakers)
	}
	return fmt.Sprintf("%d speakers, %d segments", n, len(r.Segments))
}
