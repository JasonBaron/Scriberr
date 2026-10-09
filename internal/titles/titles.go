// Package titles builds standardized working titles and tag suggestions for
// recordings from their AI summary.
package titles

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	// EnvFormat sets the title pattern. Placeholders: {date} (YYYY-MM-DD) and
	// {topic}. Default "{date} {topic}".
	EnvFormat     = "SCRIBERR_TITLE_FORMAT"
	defaultFormat = "{date} {topic}"
	// EnvSuggest ("false" to disable) controls title and tag suggestions
	// after each summary.
	EnvSuggest = "SCRIBERR_SUGGEST_TITLES"

	maxTopicLen = 80
	maxTags     = 5
	maxTagLen   = 30
)

// Pattern returns the configured title pattern.
func Pattern() string {
	if p := strings.TrimSpace(os.Getenv(EnvFormat)); p != "" {
		return p
	}
	return defaultFormat
}

// Format fills the pattern with the date and topic.
func Format(pattern string, date time.Time, topic string) string {
	t := strings.ReplaceAll(pattern, "{date}", date.Format("2006-01-02"))
	t = strings.ReplaceAll(t, "{topic}", topic)
	return strings.Join(strings.Fields(t), " ")
}

var (
	uuidRe      = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	recordingRe = regexp.MustCompile(`(?i)^(new )?(recording|voice memo|audio|untitled)( ?\(?\d+\)?)?$`)
	mediaExt    = map[string]bool{
		".m4a": true, ".mp3": true, ".wav": true, ".flac": true, ".aac": true, ".ogg": true,
		".opus": true, ".webm": true, ".mp4": true, ".mov": true, ".mkv": true, ".wma": true, ".aiff": true,
	}
)

// IsPlaceholder reports whether a title is something the user never chose:
// empty, the upload's file name, a UUID, or a recorder default such as
// "New Recording 12". Those are safe to replace automatically.
func IsPlaceholder(title, audioPath string) bool {
	t := strings.TrimSpace(title)
	if t == "" {
		return true
	}
	stem := t
	if ext := strings.ToLower(filepath.Ext(t)); mediaExt[ext] {
		return true // still looks like a file name
	} else if ext != "" && len(ext) <= 6 {
		stem = strings.TrimSuffix(t, filepath.Ext(t))
	}
	if uuidRe.MatchString(stem) || recordingRe.MatchString(stem) {
		return true
	}
	if audioPath != "" {
		base := filepath.Base(audioPath)
		if strings.EqualFold(t, base) || strings.EqualFold(t, strings.TrimSuffix(base, filepath.Ext(base))) {
			return true
		}
	}
	return false
}

// maxVocabulary caps how many existing tags are offered to the model.
const maxVocabulary = 60

// Prompt asks for a topic, a one-sentence brief and tags as JSON, based on
// the summary. vocabulary lists tags already in use, most used first; the
// model is told to reuse them so tags stay consistent across recordings.
func Prompt(summary string, vocabulary []string) string {
	var b strings.Builder
	b.WriteString(`Read this summary of a recording and return JSON only, no other text:
{"topic": "...", "brief": "...", "tags": ["...", "..."]}

topic: 4 to 8 words naming what the recording is specifically about. Title Case. No date, no quotes, no trailing punctuation. Avoid generic words like Recording, Conversation, Discussion, Meeting, Summary.
brief: one plain sentence of at most 25 words saying what the recording covers, for a list view.
tags: 3 to 5 short lowercase tags (1 to 2 words each, singular nouns) for filing and search, most specific first.
`)
	if len(vocabulary) > maxVocabulary {
		vocabulary = vocabulary[:maxVocabulary]
	}
	if len(vocabulary) > 0 {
		b.WriteString("Existing tags: reuse these, spelled exactly as shown, whenever one fits. Add a new tag only when none fits.\n")
		b.WriteString(strings.Join(vocabulary, ", "))
		b.WriteString("\n")
	}
	b.WriteString("\nSummary:\n")
	b.WriteString(summary)
	return b.String()
}

// Suggestion is what the model proposed for a recording.
type Suggestion struct {
	Topic string
	Brief string
	Tags  []string
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// Parse extracts and cleans the suggestion from a model reply. It tolerates
// code fences, leading text and thinking blocks around the JSON. Tags are
// standardized against vocabulary (see Standardize).
func Parse(raw string, vocabulary []string) (Suggestion, error) {
	raw = stripThinking(raw)
	m := jsonObjRe.FindString(raw)
	if m == "" {
		return Suggestion{}, errors.New("no JSON object in reply")
	}
	var v struct {
		Topic   string   `json:"topic"`
		Title   string   `json:"title"`
		Brief   string   `json:"brief"`
		Summary string   `json:"summary"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(m), &v); err != nil {
		return Suggestion{}, err
	}
	if v.Topic == "" {
		v.Topic = v.Title
	}
	if v.Brief == "" {
		v.Brief = v.Summary
	}
	out := Suggestion{Topic: cleanTopic(v.Topic), Brief: cleanBrief(v.Brief), Tags: Standardize(v.Tags, vocabulary, maxTags)}
	if out.Topic == "" {
		return Suggestion{}, errors.New("empty topic")
	}
	return out, nil
}

func stripThinking(s string) string {
	for {
		i := strings.Index(s, "<think>")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "</think>")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+len("</think>"):]
	}
}

func cleanTopic(t string) string {
	t = strings.TrimSpace(t)
	t = strings.Trim(t, "\"'`*#")
	t = strings.Join(strings.Fields(t), " ")
	t = strings.TrimRightFunc(t, func(r rune) bool { return unicode.IsPunct(r) && r != ')' })
	if len(t) > maxTopicLen {
		cut := strings.LastIndex(t[:maxTopicLen], " ")
		if cut < maxTopicLen/2 {
			cut = maxTopicLen
		}
		t = strings.TrimSpace(t[:cut])
	}
	return t
}

const maxBriefLen = 220

func cleanBrief(t string) string {
	t = strings.Join(strings.Fields(strings.Trim(strings.TrimSpace(t), "\"'`*")), " ")
	if len(t) > maxBriefLen {
		cut := strings.LastIndex(t[:maxBriefLen], " ")
		if cut < maxBriefLen/2 {
			cut = maxBriefLen
		}
		t = strings.TrimRight(t[:cut], ",;:") + "..."
	}
	return t
}

// MaxUserTags and MaxUserTagLen bound tags edited by hand.
const (
	MaxUserTags   = 20
	MaxUserTagLen = 40
)

// NormalizeTag puts a tag in the standard form: lowercase, words separated
// by single spaces, no leading #, quotes, underscores or trailing punctuation.
func NormalizeTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	tag = strings.Trim(tag, "#\"'`")
	tag = strings.ReplaceAll(tag, "_", " ")
	tag = strings.Join(strings.Fields(tag), " ")
	return strings.TrimRightFunc(tag, func(r rune) bool { return unicode.IsPunct(r) })
}

// variants returns singular and plural spellings to match a tag against
// existing ones ("meeting" and "meetings", "family" and "families").
func variants(tag string) []string {
	v := []string{tag + "s", tag + "es"}
	switch {
	case strings.HasSuffix(tag, "ies"):
		v = append(v, strings.TrimSuffix(tag, "ies")+"y")
	case strings.HasSuffix(tag, "es"):
		v = append(v, strings.TrimSuffix(tag, "es"), strings.TrimSuffix(tag, "s"))
	case strings.HasSuffix(tag, "s"):
		v = append(v, strings.TrimSuffix(tag, "s"))
	case strings.HasSuffix(tag, "y"):
		v = append(v, strings.TrimSuffix(tag, "y")+"ies")
	}
	return v
}

// Standardize normalizes tags, maps each onto an existing tag in
// vocabulary when they differ only in case, spacing or singular/plural,
// drops duplicates and keeps at most limit.
func Standardize(tags, vocabulary []string, limit int) []string {
	known := map[string]string{}
	for _, v := range vocabulary {
		if n := NormalizeTag(v); n != "" {
			known[n] = n
			known[strings.ReplaceAll(n, "-", " ")] = n
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, tag := range tags {
		tag = NormalizeTag(tag)
		if tag == "" {
			continue
		}
		if k, ok := known[tag]; ok {
			tag = k
		} else if k, ok := known[strings.ReplaceAll(tag, "-", " ")]; ok {
			tag = k
		} else {
			for _, v := range variants(tag) {
				if k, ok := known[v]; ok {
					tag = k
					break
				}
			}
		}
		if len(tag) > maxTagLen {
			continue
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
		if len(out) == limit {
			break
		}
	}
	return out
}

// CleanUserTags normalizes tags entered by hand, keeping their order.
func CleanUserTags(tags []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, tag := range tags {
		tag = NormalizeTag(tag)
		if tag == "" || len(tag) > MaxUserTagLen || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
		if len(out) == MaxUserTags {
			break
		}
	}
	return out
}

// TagMatches reports whether tag contains want as whole words, ignoring
// case, spacing and singular/plural: "therapy" matches "couples therapy",
// "relationship" matches "relationships", "art" does not match "party".
func TagMatches(want, tag string) bool {
	w := strings.Fields(NormalizeTag(want))
	t := strings.Fields(NormalizeTag(tag))
	if len(w) == 0 || len(w) > len(t) {
		return false
	}
	for i := 0; i+len(w) <= len(t); i++ {
		ok := true
		for j := range w {
			if !sameWord(w[j], t[i+j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	for _, v := range variants(a) {
		if v == b {
			return true
		}
	}
	return false
}
