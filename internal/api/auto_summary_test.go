package api

import (
	"strings"
	"testing"
)

func TestBuildSummaryContent(t *testing.T) {
	tr := `{"text":" hello there. hi.","segments":[{"text":" hello there.","speaker":"SPEAKER_00"},{"text":" hi.","speaker":"SPEAKER_01"},{"text":"x"}]}`

	plain, err := buildSummaryContent(tr, false, nil, "Summarize.")
	if err != nil {
		t.Fatal(err)
	}
	if plain != "Transcript:\n hello there. hi.\n\nInstructions:\nSummarize." {
		t.Errorf("plain = %q", plain)
	}

	labeled, err := buildSummaryContent(tr, true, map[string]string{"SPEAKER_00": "Jason"}, "Summarize.")
	if err != nil {
		t.Fatal(err)
	}
	want := "[Jason] hello there.\n[SPEAKER_01] hi.\n[UNKNOWN] x"
	if !strings.Contains(labeled, want) || !strings.HasPrefix(labeled, "Transcript (with speaker labels") {
		t.Errorf("labeled = %q", labeled)
	}

	if _, err := buildSummaryContent(`{"text":"  "}`, false, nil, "x"); err == nil {
		t.Error("empty transcript should fail")
	}
}
