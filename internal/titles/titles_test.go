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

func TestParse(t *testing.T) {
	raw := "<think>let me think</think>Sure! ```json\n{\"topic\": \"\\\"Family History and Dad's Friends.\\\"\", \"brief\": \"  Two siblings compare   their parents' friendships.\", \"tags\": [\"Family\", \"family\", \"#Friendship\", \"caregiving\", \"bipolar disorder\", \"covid\", \"extra\"]}\n```"
	s, err := Parse(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Topic != "Family History and Dad's Friends" {
		t.Errorf("topic = %q", s.Topic)
	}
	if s.Brief != "Two siblings compare their parents' friendships." {
		t.Errorf("brief = %q", s.Brief)
	}
	want := []string{"family", "friendship", "caregiving", "bipolar disorder", "covid"}
	if strings.Join(s.Tags, "|") != strings.Join(want, "|") {
		t.Errorf("tags = %v, want %v", s.Tags, want)
	}

	if _, err := Parse("no json here", nil); err == nil {
		t.Error("expected error without JSON")
	}
	if s, err := Parse(`{"title": "Fallback Title Field"}`, nil); err != nil || s.Topic != "Fallback Title Field" {
		t.Errorf("title fallback: %q %v", s.Topic, err)
	}
	long := strings.Repeat("word ", 30)
	if s, _ := Parse(`{"topic": "`+long+`", "brief": "`+strings.Repeat("word ", 80)+`"}`, nil); len(s.Topic) > maxTopicLen || len(s.Brief) > maxBriefLen+3 {
		t.Errorf("not shortened: topic %d, brief %d chars", len(s.Topic), len(s.Brief))
	}
}

func TestStandardize(t *testing.T) {
	vocab := []string{"family relationships", "meeting", "budget", "follow-up", "therapy"}
	got := Standardize([]string{"Family_Relationships", "meetings", "Budgets!", "follow up", "Therapy", "new topic", "budget"}, vocab, 5)
	want := []string{"family relationships", "meeting", "budget", "follow-up", "therapy"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := Standardize([]string{"families"}, []string{"family"}, 5); len(got) != 1 || got[0] != "family" {
		t.Errorf("plural ies: %v", got)
	}
}

func TestPromptVocabulary(t *testing.T) {
	if p := Prompt("x", nil); strings.Contains(p, "Existing tags") {
		t.Error("no vocabulary section expected")
	}
	if p := Prompt("x", []string{"family", "budget"}); !strings.Contains(p, "family, budget") {
		t.Error("vocabulary missing from prompt")
	}
}

func TestCleanUserTags(t *testing.T) {
	got := CleanUserTags([]string{" Work ", "work", "", "#Q4 Planning", strings.Repeat("x", 50)})
	if strings.Join(got, "|") != "work|q4 planning" {
		t.Errorf("got %v", got)
	}
}

func TestTagMatches(t *testing.T) {
	cases := []struct {
		want, tag string
		ok        bool
	}{
		{"therapy", "Couples Therapy", true},
		{"relationship", "relationships", true},
		{"family", "families", true},
		{"couples therapy", "couples therapy", true},
		{"counseling", "counseling session", true},
		{"art", "party", false},
		{"couples therapy", "therapy", false},
		{"", "therapy", false},
	}
	for _, c := range cases {
		if got := TagMatches(c.want, c.tag); got != c.ok {
			t.Errorf("TagMatches(%q, %q) = %v", c.want, c.tag, got)
		}
	}
}
