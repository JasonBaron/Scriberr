package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"scriberr/internal/llm"
	"scriberr/pkg/logger"
)

// Long transcripts do not fit the model's context on a 12 GB card: Ollama
// then silently drops the start of the prompt, so a 60 minute session was
// summarized from its second half only. Transcripts longer than the chunk
// size are first turned into notes part by part, and the template runs on
// the notes.
const (
	// EnvSummaryChunkChars sets the longest transcript (in characters) sent
	// in one request, and the size of each part when it is split.
	EnvSummaryChunkChars = "SCRIBERR_SUMMARY_CHUNK_CHARS"
	defaultChunkChars    = 36000 // about 12k tokens
	minChunkChars        = 4000

	instructionsMarker = "\n\nInstructions:\n"
)

func summaryChunkChars() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvSummaryChunkChars))); err == nil && v >= minChunkChars {
		return v
	}
	return defaultChunkChars
}

// splitSummaryContent separates a summary prompt built by
// buildSummaryContent (or the web UI) into the transcript label, the
// transcript and the instructions. ok is false when the shape is unknown.
func splitSummaryContent(content string) (label, transcript, instructions string, ok bool) {
	i := strings.Index(content, instructionsMarker)
	if i < 0 {
		return "", "", "", false
	}
	head, instructions := content[:i], content[i+len(instructionsMarker):]
	nl := strings.Index(head, "\n")
	if nl < 0 || !strings.HasPrefix(head, "Transcript") {
		return "", "", "", false
	}
	return head[:nl], head[nl+1:], instructions, true
}

// isLongTranscript reports whether content holds a transcript longer than
// one request should carry.
func isLongTranscript(content string) bool {
	_, transcript, _, ok := splitSummaryContent(content)
	return ok && len(transcript) > summaryChunkChars()
}

// chunkTranscript splits text into parts of at most size characters, at
// line breaks where possible, else at spaces.
func chunkTranscript(text string, size int) []string {
	var parts []string
	var cur strings.Builder
	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			parts = append(parts, strings.TrimSpace(cur.String()))
		}
		cur.Reset()
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		for len(line) > size {
			cut := strings.LastIndex(line[:size], " ")
			if cut <= 0 {
				cut = size
			}
			if cur.Len() > 0 {
				flush()
			}
			parts = append(parts, strings.TrimSpace(line[:cut]))
			line = line[cut:]
		}
		if cur.Len()+len(line) > size {
			flush()
		}
		cur.WriteString(line)
	}
	flush()
	return parts
}

const notesPrompt = `This is part %d of %d of a long transcript, in order. Write detailed notes on this part only, for someone who will combine the notes from every part into one summary later.

Keep: who said what (use the speaker labels), the topics in order, decisions, agreements, action items, open questions, feelings people stated, names of places, tests, medications and treatments, and short exact quotes that matter. Use only what is in this part. No introduction or closing remarks. Bullets are fine.

`

// condenseLongTranscript returns content unchanged when the transcript is
// short enough. Otherwise it writes notes for each part of the transcript
// and returns the same instructions applied to those notes.
func (h *Handler) condenseLongTranscript(ctx context.Context, svc llm.Service, model, content, jobID string) (string, error) {
	label, transcript, instructions, ok := splitSummaryContent(content)
	size := summaryChunkChars()
	if !ok || len(transcript) <= size {
		return content, nil
	}
	parts := chunkTranscript(transcript, size)
	start := time.Now()
	logger.Info("Long transcript: writing notes part by part", "job_id", jobID, "chars", len(transcript), "parts", len(parts))
	nctx := llm.WithDeterministic(llm.WithThinking(ctx, false))
	var notes strings.Builder
	for i, part := range parts {
		pctx, cancel := context.WithTimeout(nctx, 20*time.Minute)
		resp, err := svc.ChatCompletion(pctx, model, []llm.ChatMessage{{Role: "user",
			Content: fmt.Sprintf(notesPrompt, i+1, len(parts)) + label + "\n" + part}}, 0)
		cancel()
		if err != nil || resp == nil || len(resp.Choices) == 0 {
			if err == nil {
				err = errors.New("empty reply")
			}
			return "", fmt.Errorf("notes for part %d of %d: %w", i+1, len(parts), err)
		}
		fmt.Fprintf(&notes, "## Part %d of %d\n%s\n\n", i+1, len(parts), strings.TrimSpace(resp.Choices[0].Message.Content))
	}
	logger.Info("Long transcript: notes ready", "job_id", jobID, "parts", len(parts), "duration", time.Since(start).Round(time.Second))
	return "Notes from a long transcript, in order. It was too long to read in one pass, so each part was noted separately; treat the notes as the transcript.\n\n" +
		strings.TrimSpace(notes.String()) + instructionsMarker + instructions, nil
}
