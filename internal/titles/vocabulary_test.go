package titles

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultVocabularyParses(t *testing.T) {
	v := NewVocabulary("", "", "")
	if len(v.Types) != 9 || len(v.Topics) != 23 {
		t.Fatalf("types %d topics %d", len(v.Types), len(v.Topics))
	}
	// Every synonym must point at a vocabulary tag.
	for old, target := range v.Synonyms {
		if v.ResolveType(target) == "" && v.ResolveTopic(target) == "" {
			t.Errorf("synonym %q points at unknown tag %q", old, target)
		}
	}
}

func TestParseStrict(t *testing.T) {
	v := NewVocabulary("", "", "")
	raw := "<think>x</think>```json\n{\"topic\":\"Voice Therapy Plan\",\"brief\":\"Plan.\",\"type\":\"Doctor Visit\",\"topics\":[\"Swallowing\",\"medications\",\"made up\",\"voice and swallowing\",\"health\",\"legal\"],\"pii\":\"true\"}\n```"
	c, err := v.ParseStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "medical appointment" || !c.PII {
		t.Errorf("type %q pii %v", c.Type, c.PII)
	}
	if want := []string{"voice and swallowing", "medication and treatment", "health"}; !reflect.DeepEqual(c.Topics, want) {
		t.Errorf("topics %v", c.Topics)
	}

	// Old-style reply with only tags.
	c, err = v.ParseStrict(`{"topic":"Weekly Check In","tags":["couples counseling","trust issues","argument"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "couples therapy" || !reflect.DeepEqual(c.Topics, []string{"relationships", "conflict"}) {
		t.Errorf("got %+v", c)
	}
}

func TestVocabularyTags(t *testing.T) {
	v := NewVocabulary("", "", "")
	got := v.Tags(Classified{Type: "conversation", Topics: []string{"technology"}}, Flags{})
	if !reflect.DeepEqual(got, []string{"conversation", "technology"}) {
		t.Errorf("plain: %v", got)
	}
	got = v.Tags(Classified{Type: "conversation", Topics: []string{"finances"}}, Flags{})
	if !reflect.DeepEqual(got, []string{"conversation", "finances", "sensitive"}) {
		t.Errorf("sensitive topic: %v", got)
	}
	got = v.Tags(Classified{Type: "personal note", Topics: []string{"personal growth"}}, Flags{YouTube: true, PII: true})
	if !reflect.DeepEqual(got, []string{"media", "personal growth", "youtube", "sensitive", "pii"}) {
		t.Errorf("youtube pii: %v", got)
	}
	got = v.Tags(Classified{Type: "conversation", Topics: []string{"family"}, PII: true}, Flags{})
	if !reflect.DeepEqual(got, []string{"conversation", "family"}) {
		t.Errorf("model PII is ignored: %v", got)
	}
	got = v.Tags(Classified{Type: "couples therapy", Topics: []string{"relationships"}}, Flags{})
	if !reflect.DeepEqual(got, []string{"couples therapy", "relationships", "sensitive"}) {
		t.Errorf("sensitive type: %v", got)
	}
}

func TestCustomVocabulary(t *testing.T) {
	v := NewVocabulary("call: phone call\n# comment\n!visit: doctor", "Gardening: plants\ngardening: dup", "plants = gardening")
	if len(v.Types) != 2 || len(v.Topics) != 1 || !v.Types[1].Sensitive {
		t.Fatalf("%+v", v)
	}
	if v.ResolveTopic("Plants") != "gardening" || v.ResolveTopic("finances") != "" {
		t.Error("resolve")
	}
}

func TestWithFlags(t *testing.T) {
	got := WithFlags([]string{"stoicism", "youtube"}, Flags{YouTube: true, PII: true})
	if !reflect.DeepEqual(got, []string{"stoicism", "youtube", "sensitive", "pii"}) {
		t.Errorf("%v", got)
	}
}

func TestYouTubeTitle(t *testing.T) {
	d := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if got := YouTube(d, "  Embracing   Uncertainty "); got != "2026-10-09 YouTube: Embracing Uncertainty" {
		t.Error(got)
	}
	if got := YouTube(d, "YouTube Audio"); got != "2026-10-09 YouTube: Video" {
		t.Error(got)
	}
}
