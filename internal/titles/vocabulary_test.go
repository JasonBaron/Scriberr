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
	if p := v.StrictPrompt("s", "", "", "", 0, nil); strings.Contains(p, "keywords") {
		t.Error("no keywords line when disabled")
	}
	p := v.StrictPrompt("s", "Therapy Session (Jason)", "[S1] How have you been since our last session?", "individual therapy", 3, []string{"stoicism"})
	if !strings.Contains(p, "1 to 3 specific") || !strings.Contains(p, "stoicism") {
		t.Error("keywords line and known keywords expected")
	}
	if !strings.Contains(p, "already known from its name: individual therapy") {
		t.Error("known type expected in prompt")
	}
	if !strings.Contains(p, "Therapy Session (Jason)") || !strings.Contains(p, "since our last session") {
		t.Error("name and transcript opening expected")
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

func TestFoldStems(t *testing.T) {
	got := foldStems([]string{"defensiveness", "defensive", "insularity", "acid reflux"}, []string{"defensive", "insular"})
	if !reflect.DeepEqual(got, []string{"defensive", "insular", "acid reflux"}) {
		t.Errorf("%v", got)
	}
	v := NewVocabulary("", "", "")
	tags, _ := v.Assemble(Classified{Type: "conversation", Topics: []string{"family"}, Keywords: []string{"Defensiveness"}}, Flags{}, 2, []string{"defensive"})
	if !reflect.DeepEqual(tags, []string{"conversation", "family", "defensive"}) {
		t.Errorf("%v", tags)
	}
}

func TestTypeFromName(t *testing.T) {
	v := NewVocabulary("", "", "").WithNameHints("")
	cases := map[string]string{
		"20260603 - Therapy Session (Jason) - 13-01-00.m4a":    "individual therapy",
		"20260520 - Therapy Discussion (Jason) - 13-01-23.m4a": "individual therapy",
		"Couples Therapy 2026-05-27.m4a":                       "couples therapy",
		"2026-07-22 Speech Therapy follow-up.m4a":              "medical appointment",
		"ENT appointment.m4a":                                  "medical appointment",
		"20260504 - Steph Jason Conversation - 18-53-05.m4a":   "",
		"BENCH 5m | large-v3 | diarize=True | 20261009-170136": "",
		"Gentle reminder":                                      "",
	}
	for name, want := range cases {
		if got := v.TypeFromName(name); got != want {
			t.Errorf("TypeFromName(%q) = %q, want %q", name, got, want)
		}
	}
	tags, _ := v.Assemble(Classified{Type: "relationship talk", Topics: []string{"mental health"}}, Flags{NameType: "individual therapy"}, 2, nil)
	if tags[0] != "individual therapy" {
		t.Errorf("name type should win: %v", tags)
	}
}

func TestPrefixTopic(t *testing.T) {
	v := NewVocabulary("", "", "").WithTitlePrefixes("")
	if got := v.PrefixTopic("individual therapy", "Boundaries With Family"); got != "Therapy: Boundaries With Family" {
		t.Error(got)
	}
	if got := v.PrefixTopic("individual therapy", "Therapy: Already"); got != "Therapy: Already" {
		t.Error(got)
	}
	if got := v.PrefixTopic("conversation", "Weekend Plans"); got != "Weekend Plans" {
		t.Error(got)
	}
	c := NewVocabulary("", "", "").WithTitlePrefixes("work meeting = Meeting:\nnot a type = X")
	if c.PrefixTopic("work meeting", "Budget") != "Meeting: Budget" || len(c.TitlePrefixes) != 1 {
		t.Errorf("%v", c.TitlePrefixes)
	}
}
