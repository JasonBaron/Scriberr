package speakers

import (
	"encoding/json"
	"strings"
	"testing"

	"scriberr/internal/transcription/interfaces"
)

func sp(s string) *string { return &s }

func w(word string, start, end float64, speaker string) interfaces.TranscriptWord {
	var p *string
	if speaker != "" {
		p = sp(speaker)
	}
	return interfaces.TranscriptWord{Word: word, Start: start, End: end, Speaker: p}
}

func TestSplitAtSpeakerChange(t *testing.T) {
	segs := []interfaces.TranscriptSegment{
		{Start: 0, End: 6, Text: " How are you? Good, thanks. And you?", Speaker: sp("A")},
		{Start: 6, End: 8, Text: " Fine.", Speaker: sp("A")},
	}
	words := []interfaces.TranscriptWord{
		w("How", 0, 0.4, "A"), w("are", 0.4, 0.6, "A"), w("you?", 0.6, 1.0, "A"),
		w("Good,", 1.5, 2.0, "B"), w("thanks.", 2.0, 2.6, "B"), w("And", 3.0, 3.3, "B"), w("you?", 3.3, 3.8, "B"),
		w("Fine.", 6.2, 6.9, "A"),
	}
	out, changed := Split(segs, words)
	if !changed || len(out) != 3 {
		t.Fatalf("changed=%v %+v", changed, out)
	}
	if out[0].Text != "How are you?" || *out[0].Speaker != "A" || out[0].Start != 0 || out[0].End != 1.0 {
		t.Errorf("first %+v", out[0])
	}
	if out[1].Text != "Good, thanks. And you?" || *out[1].Speaker != "B" || out[1].Start != 1.5 || out[1].End != 6 {
		t.Errorf("second %+v", out[1])
	}
	if out[2].Text != " Fine." || *out[2].Speaker != "A" {
		t.Errorf("unsplit segment should keep its text %+v", out[2])
	}
	// Idempotent
	if _, again := Split(out, words); again {
		t.Error("second run should change nothing")
	}
}

func TestSplitKeepsBlipsAndUnlabeledWords(t *testing.T) {
	segs := []interfaces.TranscriptSegment{{Start: 0, End: 3, Text: "one two three four", Speaker: sp("A")}}
	words := []interfaces.TranscriptWord{
		w("one", 0, 0.5, "A"), w("two", 0.5, 0.6, "B"), // 0.1s blip
		w("three", 0.7, 1.2, ""), // no speaker
		w("4", 0, 0, ""),         // no timing (numbers in WhisperX)
		w("four", 1.5, 2.0, "A"),
	}
	out, changed := Split(segs, words)
	if changed || len(out) != 1 || out[0].Text != "one two three four" {
		t.Fatalf("changed=%v %+v", changed, out)
	}
	// Segment labeled by majority but all words say B: the label follows the words
	segs[0].Speaker = sp("C")
	words = []interfaces.TranscriptWord{w("one", 0, 0.5, "B"), w("two", 0.5, 1, "B")}
	out, changed = Split(segs, words)
	if !changed || *out[0].Speaker != "B" || out[0].Text != "one two three four" {
		t.Fatalf("%+v", out)
	}
}

func TestSplitWithoutWordSpeakers(t *testing.T) {
	segs := []interfaces.TranscriptSegment{{Start: 0, End: 1, Text: "hi"}}
	if _, changed := Split(segs, []interfaces.TranscriptWord{w("hi", 0, 1, "")}); changed {
		t.Fatal("nothing to do without word speakers")
	}
}

func TestRewriteJSONKeepsOtherFields(t *testing.T) {
	raw := `{"text":"a b","language":"en","segments":[{"start":0,"end":2,"text":"a b","speaker":"A"}],` +
		`"word_segments":[{"start":0,"end":0.5,"word":"a","score":0.9,"speaker":"A"},{"start":1,"end":1.5,"word":"b","score":0.9,"speaker":"B"}],"custom":{"x":1}}`
	out, changed, err := RewriteJSON(raw)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["custom"]) != `{"x":1}` || !strings.Contains(string(doc["word_segments"]), `"score":0.9`) {
		t.Errorf("fields lost: %s", out)
	}
	var segs []interfaces.TranscriptSegment
	_ = json.Unmarshal(doc["segments"], &segs)
	if len(segs) != 2 || *segs[1].Speaker != "B" {
		t.Errorf("%+v", segs)
	}
	if same, changed, _ := RewriteJSON(out); changed || same != out {
		t.Error("rewrite should be idempotent")
	}
	if _, changed, err := RewriteJSON(`{"text":"x"}`); changed || err != nil {
		t.Error("transcripts without words are left alone")
	}
}
