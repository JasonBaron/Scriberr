package api

import (
	"strings"
	"testing"
)

func TestSplitSummaryContent(t *testing.T) {
	content, err := buildSummaryContent(`{"text":"a","segments":[{"text":"hello","speaker":"S1"},{"text":"there","speaker":"S2"}]}`, true, map[string]string{"S1": "Jason"}, "Summarize.")
	if err != nil {
		t.Fatal(err)
	}
	label, transcript, instructions, ok := splitSummaryContent(content)
	if !ok || !strings.HasPrefix(label, "Transcript (with speaker labels") || transcript != "[Jason] hello\n[S2] there" || instructions != "Summarize." {
		t.Fatalf("%v %q %q %q", ok, label, transcript, instructions)
	}
	if _, _, _, ok := splitSummaryContent("no marker here"); ok {
		t.Error("unknown shape should not split")
	}
}

func TestChunkTranscript(t *testing.T) {
	text := strings.Repeat("[A] "+strings.Repeat("word ", 20)+"\n", 50) // 50 lines of ~105 chars
	parts := chunkTranscript(text, 1000)
	if len(parts) < 5 || len(parts) > 7 {
		t.Fatalf("parts %d", len(parts))
	}
	total := 0
	for _, p := range parts {
		if len(p) > 1000 {
			t.Errorf("part too long: %d", len(p))
		}
		if !strings.HasPrefix(p, "[A]") {
			t.Errorf("part should start at a line: %q", p[:10])
		}
		total += strings.Count(p, "[A]")
	}
	if total != 50 {
		t.Errorf("lines kept %d", total)
	}
	// A single line longer than the size is cut at spaces
	long := chunkTranscript(strings.Repeat("abc ", 1000), 1000)
	for _, p := range long {
		if len(p) > 1000 {
			t.Errorf("long line part %d", len(p))
		}
	}
	if strings.Join(long, " ") != strings.TrimSpace(strings.Repeat("abc ", 1000)) {
		t.Error("text lost while cutting a long line")
	}
}
