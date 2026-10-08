package titles

import (
	"strings"
	"testing"
	"time"
)

func TestIsPlaceholder(t *testing.T) {
	audio := "/app/data/uploads/a607afce-4335-4b29-8a4e-f9cb553f8f3a.m4a"
	cases := map[string]bool{
		"":                                     true,
		"   ":                                  true,
		"New Recording 12.m4a":                 true,
		"New Recording 12":                     true,
		"Recording":                            true,
		"Untitled":                             true,
		"voice memo (3)":                       true,
		"meeting-notes.mp3":                    true,
		"a607afce-4335-4b29-8a4e-f9cb553f8f3a": true,
		"a607afce-4335-4b29-8a4e-f9cb553f8f3a.m4a":     true,
		"Q3 planning with Sam":                         false,
		"BENCH 5m | Whisper large-v3 fp16 b4":          false,
		"Stop Waiting for Life to Work Out | STOICISM": false,
		"Dr. Smith follow-up":                          false,
	}
	for title, want := range cases {
		if got := IsPlaceholder(title, audio); got != want {
			t.Errorf("IsPlaceholder(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestFormat(t *testing.T) {
	d := time.Date(2026, 10, 8, 23, 0, 0, 0, time.UTC)
	if got := Format("{date} {topic}", d, "Family History and Dad's Friends"); got != "2026-10-08 Family History and Dad's Friends" {
		t.Errorf("got %q", got)
	}
	if got := Format("{topic}", d, "Topic Only"); got != "Topic Only" {
		t.Errorf("got %q", got)
	}
	t.Setenv(EnvFormat, "")
	if Pattern() != "{date} {topic}" {
		t.Error("default pattern")
	}
}

func TestParseSuggestion(t *testing.T) {
	raw := "<think>let me think</think>Sure! ```json\n{\"topic\": \"\\\"Family History and Dad's Friends.\\\"\", \"tags\": [\"Family\", \"family\", \"#Friendship\", \"caregiving\", \"bipolar disorder\", \"covid\", \"extra\"]}\n```"
	topic, tags, err := ParseSuggestion(raw)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "Family History and Dad's Friends" {
		t.Errorf("topic = %q", topic)
	}
	want := []string{"family", "friendship", "caregiving", "bipolar disorder", "covid"}
	if strings.Join(tags, "|") != strings.Join(want, "|") {
		t.Errorf("tags = %v, want %v", tags, want)
	}

	if _, _, err := ParseSuggestion("no json here"); err == nil {
		t.Error("expected error without JSON")
	}
	if topic, _, err := ParseSuggestion(`{"title": "Fallback Title Field"}`); err != nil || topic != "Fallback Title Field" {
		t.Errorf("title fallback: %q %v", topic, err)
	}
	long := strings.Repeat("word ", 30)
	if topic, _, _ := ParseSuggestion(`{"topic": "` + long + `"}`); len(topic) > maxTopicLen {
		t.Errorf("topic not shortened: %d chars", len(topic))
	}
}
