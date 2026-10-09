// Package speakers fixes speaker labels on transcript segments.
//
// Diarization gives each word its own speaker, but each segment (a sentence
// or so from the speech model) got a single speaker: the one who talked
// most during it. When two people trade short turns inside one segment,
// the whole exchange was shown under one name. Split cuts such segments
// where the word speakers change, so every segment has one speaker.
package speakers

import (
	"encoding/json"
	"strings"

	"scriberr/internal/transcription/interfaces"
)

// A single word given to another speaker in the middle of someone's turn,
// shorter than this, is treated as diarization noise and kept with its
// neighbors. Real interjections ("yeah", "mm-hmm") are longer.
const blipSeconds = 0.2

func timed(w interfaces.TranscriptWord) bool { return w.End > 0 && w.End >= w.Start }

func speakerOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// assignWords returns, for each segment, the indexes of its words. Words
// are walked in order and move to the next segment once they start at or
// after that segment's start; words without timings stay with the current
// segment.
func assignWords(segs []interfaces.TranscriptSegment, words []interfaces.TranscriptWord) [][]int {
	out := make([][]int, len(segs))
	if len(segs) == 0 {
		return out
	}
	si := 0
	for wi, w := range words {
		if timed(w) {
			for si+1 < len(segs) && w.Start >= segs[si+1].Start-0.01 {
				si++
			}
		}
		out[si] = append(out[si], wi)
	}
	return out
}

type run struct {
	speaker string
	words   []int
}

// runs groups a segment's words by speaker. Words without a speaker join
// the run they are in; blips are folded into the surrounding run.
func runs(idx []int, words []interfaces.TranscriptWord) []run {
	var rs []run
	for _, wi := range idx {
		sp := speakerOf(words[wi].Speaker)
		if len(rs) > 0 && (sp == "" || sp == rs[len(rs)-1].speaker) {
			rs[len(rs)-1].words = append(rs[len(rs)-1].words, wi)
			continue
		}
		if len(rs) == 1 && rs[0].speaker == "" {
			rs[0].speaker = sp // leading words without a speaker
			rs[0].words = append(rs[0].words, wi)
			continue
		}
		rs = append(rs, run{speaker: sp, words: []int{wi}})
	}
	// Fold blips: A [x] A -> A
	for changed := true; changed; {
		changed = false
		for i := 1; i+1 < len(rs); i++ {
			if rs[i-1].speaker != rs[i+1].speaker {
				continue
			}
			// Only words carrying a speaker count; unlabeled ones just ride along
			var labeled []interfaces.TranscriptWord
			for _, wi := range rs[i].words {
				if speakerOf(words[wi].Speaker) != "" {
					labeled = append(labeled, words[wi])
				}
			}
			if len(labeled) != 1 || (timed(labeled[0]) && labeled[0].End-labeled[0].Start >= blipSeconds) {
				continue
			}
			merged := append(append(append([]int{}, rs[i-1].words...), rs[i].words...), rs[i+1].words...)
			rs[i-1].words = merged
			rs = append(rs[:i], rs[i+2:]...)
			changed = true
			break
		}
	}
	return rs
}

func joinWords(idx []int, words []interfaces.TranscriptWord) string {
	parts := make([]string, 0, len(idx))
	for _, wi := range idx {
		if t := strings.TrimSpace(words[wi].Word); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func strPtr(s string) *string { return &s }

// Split returns the segments cut at word speaker changes, and whether
// anything changed. Segments whose words share one speaker keep their text
// and timings and take that speaker. Running it again changes nothing.
func Split(segs []interfaces.TranscriptSegment, words []interfaces.TranscriptWord) ([]interfaces.TranscriptSegment, bool) {
	hasSpeakers := false
	for _, w := range words {
		if speakerOf(w.Speaker) != "" {
			hasSpeakers = true
			break
		}
	}
	if !hasSpeakers || len(segs) == 0 {
		return segs, false
	}
	byseg := assignWords(segs, words)
	out := make([]interfaces.TranscriptSegment, 0, len(segs))
	changed := false
	for i, seg := range segs {
		rs := runs(byseg[i], words)
		if len(rs) == 0 || (len(rs) == 1 && rs[0].speaker == "") {
			out = append(out, seg)
			continue
		}
		if len(rs) == 1 {
			if speakerOf(seg.Speaker) != rs[0].speaker {
				seg.Speaker = strPtr(rs[0].speaker)
				changed = true
			}
			out = append(out, seg)
			continue
		}
		changed = true
		for ri, r := range rs {
			part := interfaces.TranscriptSegment{
				Text:     joinWords(r.words, words),
				Speaker:  strPtr(r.speaker),
				Language: seg.Language,
				Start:    seg.Start,
				End:      seg.End,
			}
			// Timings from the run's first and last timed words; the first
			// and last runs keep the segment's own edges.
			if ri > 0 {
				for _, wi := range r.words {
					if timed(words[wi]) {
						part.Start = words[wi].Start
						break
					}
				}
			}
			if ri < len(rs)-1 {
				for k := len(r.words) - 1; k >= 0; k-- {
					if w := words[r.words[k]]; timed(w) {
						part.End = w.End
						break
					}
				}
			}
			if part.Text != "" {
				out = append(out, part)
			}
		}
	}
	return out, changed
}

// Apply splits a transcript result's segments in place and reports whether
// anything changed.
func Apply(r *interfaces.TranscriptResult) bool {
	if r == nil {
		return false
	}
	segs, changed := Split(r.Segments, r.WordSegments)
	if changed {
		r.Segments = segs
	}
	return changed
}

// RewriteJSON applies Split to a stored transcript. Only "segments" is
// rewritten; every other field is kept as stored, and words are untouched
// (notes point at word positions).
func RewriteJSON(raw string) (string, bool, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw, false, err
	}
	var segs []interfaces.TranscriptSegment
	var words []interfaces.TranscriptWord
	if doc["segments"] == nil || doc["word_segments"] == nil {
		return raw, false, nil
	}
	if err := json.Unmarshal(doc["segments"], &segs); err != nil {
		return raw, false, err
	}
	if err := json.Unmarshal(doc["word_segments"], &words); err != nil {
		return raw, false, err
	}
	out, changed := Split(segs, words)
	if !changed {
		return raw, false, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return raw, false, err
	}
	doc["segments"] = b
	full, err := json.Marshal(doc)
	if err != nil {
		return raw, false, err
	}
	return string(full), true, nil
}
