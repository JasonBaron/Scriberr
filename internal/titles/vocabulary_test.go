package titles

import (
	"reflect"
	"strings"
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

func TestAssemble(t *testing.T) {
	v := NewVocabulary("", "", "")
	check := func(name string, gotT, gotF, wantT, wantF []string) {
		t.Helper()
		if !reflect.DeepEqual(gotT, wantT) || !reflect.DeepEqual(gotF, wantF) {
			t.Errorf("%s: tags %v flags %v, want %v %v", name, gotT, gotF, wantT, wantF)
		}
	}
	tg, fl := v.Assemble(Classified{Type: "conversation", Topics: []string{"technology"}}, Flags{}, 3, nil)
	check("plain", tg, fl, []string{"conversation", "technology"}, nil)

	tg, fl = v.Assemble(Classified{Type: "conversation", Topics: []string{"finances"}}, Flags{}, 3, nil)
	check("sensitive topic is a flag", tg, fl, []string{"conversation", "finances"}, []string{"sensitive"})

	tg, fl = v.Assemble(Classified{Type: "personal note", Topics: []string{"personal growth"}, Keywords: []string{"Stoicisms", "amor fati"}}, Flags{YouTube: true, PII: true}, 3, []string{"stoicism"})
	check("youtube, pii, keywords folded", tg, fl, []string{"media", "personal growth", "stoicism", "amor fati", "youtube"}, []string{"sensitive", "pii"})

	tg, fl = v.Assemble(Classified{Type: "conversation", Topics: []string{"family"}, PII: true}, Flags{}, 3, nil)
	check("model PII ignored", tg, fl, []string{"conversation", "family"}, nil)

	tg, _ = v.Assemble(Classified{Type: "conversation", Topics: []string{"family"}, Keywords: []string{"a", "b", "c", "d"}}, Flags{}, 2, nil)
	check("keyword cap", tg, nil, []string{"conversation", "family", "a", "b"}, nil)

	tg, _ = v.Assemble(Classified{Type: "conversation", Topics: []string{"family"}, Keywords: []string{"a"}}, Flags{}, 0, nil)
	check("keywords off", tg, nil, []string{"conversation", "family"}, nil)
}

func TestParseStrictKeywords(t *testing.T) {
	v := NewVocabulary("", "", "")
	c, err := v.ParseStrict(`{"topic":"Caring For A Parent","type":"conversation","topics":["family","caregiving","grief","health"],"keywords":["Bipolar Disorder","caregiving","anxiety","sensitive","pii","youtube","a very long keyword phrase here"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Topics, []string{"family", "caregiving", "grief"}) {
		t.Errorf("topics %v", c.Topics)
	}
	// caregiving is already a topic; flags, youtube and long phrases are
	// dropped; specific details stay keywords.
	if !reflect.DeepEqual(c.Keywords, []string{"bipolar disorder", "anxiety"}) {
		t.Errorf("keywords %v", c.Keywords)
	}
}

func TestPromptKeywords(t *testing.T) {
	v := NewVocabulary("", "", "")
	if p := v.StrictPrompt("s", "", 0, nil); strings.Contains(p, "keywords") {
		t.Error("no keywords line when disabled")
	}
	p := v.StrictPrompt("s", "", 3, []string{"stoicism"})
	if !strings.Contains(p, "1 to 3 specific") || !strings.Contains(p, "stoicism") {
		t.Error("keywords line and known keywords expected")
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

func TestSplitFlags(t *testing.T) {
	tg, fl := SplitFlags([]string{"stoicism", "sensitive", "youtube"}, Flags{YouTube: true, PII: true})
	if !reflect.DeepEqual(tg, []string{"stoicism", "youtube"}) || !reflect.DeepEqual(fl, []string{"sensitive", "pii"}) {
		t.Errorf("%v %v", tg, fl)
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
