// Package voiceid matches speakers across recordings by voice.
//
// Each recording's speakers get a voice print (an embedding from the
// PyAnnote speaker model). Naming a speaker adds that print to a voice
// profile for the name; later recordings whose speakers sound close enough
// to a profile get that name automatically. The same prints show when one
// label holds two voices, or one voice got two labels, so a recording can be
// flagged for a redo.
package voiceid

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

const (
	// DefaultThreshold is the lowest similarity that names a speaker.
	DefaultThreshold = 0.6
	// Margin is how far the best profile must beat the next one.
	Margin = 0.05
	// MinMatchSeconds is the least speech a speaker needs to be matched.
	MinMatchSeconds = 8.0
	// MinEnrollSeconds is the least speech a print needs to join a profile.
	MinEnrollSeconds = 10.0
	// A print teaches a voice only when its chunks agree with each other:
	// at least MinEnrollChunks of them, scoring MinEnrollScore on average
	// against the print. A short or mixed label (one person split off, or
	// two people under one label) would otherwise pull the voice off.
	MinEnrollChunks = 4
	MinEnrollScore  = 0.7

	// A chunk scoring below LowChunkScore against its own speaker likely
	// belongs to someone else.
	LowChunkScore = 0.35
	// MixedShare of a speaker's audio scoring low flags the label as mixed.
	MixedShare = 0.2
	// SameVoiceScore between two speakers of one recording means they are
	// probably one person split in two.
	SameVoiceScore = 0.75
	// MinAssessSeconds is the least speech a speaker needs to be judged.
	MinAssessSeconds = 20.0
	// Sample weights stop growing at this many seconds, so one long
	// recording does not outweigh many short ones.
	maxSampleWeight = 600.0
)

// Chunk is one embedded stretch of a speaker's audio and its similarity to
// that speaker's print.
type Chunk struct {
	Start, End, Score float64
}

// Print is one speaker's voice in one recording.
type Print struct {
	Speaker string
	Vector  []float64
	Seconds float64
	Chunks  []Chunk
}

// Profile is a named voice.
type Profile struct {
	ID     string
	Name   string
	Vector []float64
}

// Sample is one print that belongs to a profile.
type Sample struct {
	Vector  []float64
	Seconds float64
}

// Cosine returns the cosine similarity of a and b (0 when either is empty
// or they differ in length).
func Cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func normalize(v []float64) []float64 {
	var n float64
	for _, x := range v {
		n += x * x
	}
	if n == 0 {
		return v
	}
	n = math.Sqrt(n)
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

// Centroid is the weighted mean of the samples' unit vectors, as a unit
// vector. Samples of another length than the first are skipped.
func Centroid(samples []Sample) []float64 {
	var sum []float64
	for _, s := range samples {
		if len(s.Vector) == 0 {
			continue
		}
		if sum == nil {
			sum = make([]float64, len(s.Vector))
		}
		if len(s.Vector) != len(sum) {
			continue
		}
		w := math.Min(math.Max(s.Seconds, 1), maxSampleWeight)
		for i, x := range normalize(s.Vector) {
			sum[i] += w * x
		}
	}
	if sum == nil {
		return nil
	}
	return normalize(sum)
}

// Match is a speaker named by voice.
type Match struct {
	Speaker   string
	ProfileID string
	Name      string
	Score     float64
}

// Assign names speakers from profiles. Speakers in skip (already named by
// hand) are left alone, and so are profiles whose name is in taken. Each
// profile names at most one speaker per recording; the closest pairs win.
func Assign(prints []Print, profiles []Profile, threshold float64, skip, taken map[string]bool) []Match {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	type pair struct {
		sp, pr int
		score  float64
	}
	var pairs []pair
	second := map[int]float64{} // speaker -> second best score
	best := map[int]float64{}
	for si, p := range prints {
		if skip[p.Speaker] || p.Seconds < MinMatchSeconds {
			continue
		}
		for pi, pr := range profiles {
			if taken[strings.ToLower(pr.Name)] {
				continue
			}
			s := Cosine(p.Vector, pr.Vector)
			if s > best[si] {
				second[si], best[si] = best[si], s
			} else if s > second[si] {
				second[si] = s
			}
			if s >= threshold {
				pairs = append(pairs, pair{si, pi, s})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
	usedSp, usedPr := map[int]bool{}, map[int]bool{}
	var out []Match
	for _, p := range pairs {
		if usedSp[p.sp] || usedPr[p.pr] {
			continue
		}
		// The best profile for this speaker must stand out from the next
		if p.score < best[p.sp] || best[p.sp]-second[p.sp] < Margin {
			usedSp[p.sp] = true
			continue
		}
		usedSp[p.sp], usedPr[p.pr] = true, true
		out = append(out, Match{Speaker: prints[p.sp].Speaker, ProfileID: profiles[p.pr].ID, Name: profiles[p.pr].Name, Score: p.score})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Speaker < out[j].Speaker })
	return out
}

// Consistency is the duration-weighted mean score of a print's chunks
// against the print (1 = every chunk sounds the same). 0 without chunks.
func Consistency(p Print) float64 {
	var sum, total float64
	for _, c := range p.Chunks {
		d := c.End - c.Start
		sum += c.Score * d
		total += d
	}
	if total == 0 {
		return 0
	}
	return sum / total
}

// GoodSample reports whether a print is clean enough to teach a voice.
func GoodSample(p Print) bool {
	return len(p.Vector) > 0 && p.Seconds >= MinEnrollSeconds &&
		len(p.Chunks) >= MinEnrollChunks && Consistency(p) >= MinEnrollScore
}

// Problem is a reason to redo a recording's speakers.
type Problem struct {
	Kind     string   `json:"kind"` // mixed or same
	Speakers []string `json:"speakers"`
	Score    float64  `json:"score"`
	Text     string   `json:"text"`
}

// Assess looks for labels holding two voices and voices split over two
// labels.
func Assess(prints []Print) []Problem {
	var out []Problem
	for _, p := range prints {
		if p.Seconds < MinAssessSeconds || len(p.Chunks) < 4 {
			continue
		}
		var low, total float64
		for _, c := range p.Chunks {
			d := c.End - c.Start
			total += d
			if c.Score < LowChunkScore {
				low += d
			}
		}
		if total > 0 && low/total >= MixedShare {
			share := low / total
			out = append(out, Problem{Kind: "mixed", Speakers: []string{p.Speaker}, Score: round(share),
				Text: fmt.Sprintf("%s sounds like more than one person (%.0f%% of their audio)", p.Speaker, share*100)})
		}
	}
	for i := range prints {
		for j := i + 1; j < len(prints); j++ {
			a, b := prints[i], prints[j]
			if a.Seconds < MinAssessSeconds || b.Seconds < MinAssessSeconds {
				continue
			}
			if s := Cosine(a.Vector, b.Vector); s >= SameVoiceScore {
				out = append(out, Problem{Kind: "same", Speakers: []string{a.Speaker, b.Speaker}, Score: round(s),
					Text: fmt.Sprintf("%s and %s sound like the same person (%.2f)", a.Speaker, b.Speaker, s)})
			}
		}
	}
	return out
}

func round(x float64) float64 { return math.Round(x*1000) / 1000 }

var genericName = regexp.MustCompile(`(?i)^\s*(speaker|spk|person|voice|unknown|guest|other)?[\s_#-]*\d*\s*$|^\s*\?+\s*$`)

// Enrollable reports whether a speaker name is a real name worth learning
// (not "Speaker 1", "SPEAKER_00", "Unknown" or empty).
func Enrollable(name string) bool {
	return strings.TrimSpace(name) != "" && !genericName.MatchString(name)
}
