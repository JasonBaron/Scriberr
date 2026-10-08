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

// Prompt asks for a topic and tags as JSON, based on the summary.
func Prompt(summary string) string {
	return `Read this summary of a recording and return JSON only, no other text:
{"topic": "...", "tags": ["...", "..."]}

topic: 4 to 8 words naming what the recording is specifically about. Title Case. No date, no quotes, no trailing punctuation. Avoid generic words like Recording, Conversation, Discussion, Meeting, Summary.
tags: 3 to 5 short lowercase tags (1 to 3 words each) for filing and search, most specific first.

Summary:
` + summary
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// ParseSuggestion extracts and cleans the topic and tags from a model reply.
// It tolerates code fences, leading text and thinking blocks around the JSON.
func ParseSuggestion(raw string) (topic string, tags []string, err error) {
	raw = stripThinking(raw)
	m := jsonObjRe.FindString(raw)
	if m == "" {
		return "", nil, errors.New("no JSON object in reply")
	}
	var v struct {
		Topic string   `json:"topic"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(m), &v); err != nil {
		return "", nil, err
	}
	if v.Topic == "" {
		v.Topic = v.Title
	}
	topic = cleanTopic(v.Topic)
	if topic == "" {
		return "", nil, errors.New("empty topic")
	}
	return topic, cleanTags(v.Tags), nil
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

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tag := range in {
		tag = strings.ToLower(strings.TrimSpace(strings.Trim(tag, "#\"'`")))
		tag = strings.Join(strings.Fields(tag), " ")
		if tag == "" || len(tag) > maxTagLen || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
		if len(out) == maxTags {
			break
		}
	}
	return out
}
