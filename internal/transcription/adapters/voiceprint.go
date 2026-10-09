package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"scriberr/pkg/logger"
)

// VoiceTurn is one speaker turn sent to the voice print script.
type VoiceTurn struct {
	Speaker string  `json:"speaker"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
}

// VoiceChunk is one embedded stretch of a speaker's audio and how close it
// is to that speaker's overall voice print (cosine, 1 = identical).
type VoiceChunk struct {
	Start, End, Score float64
}

func (c *VoiceChunk) UnmarshalJSON(b []byte) error {
	var a [3]float64
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	c.Start, c.End, c.Score = a[0], a[1], a[2]
	return nil
}

func (c VoiceChunk) MarshalJSON() ([]byte, error) {
	return json.Marshal([3]float64{c.Start, c.End, c.Score})
}

// SpeakerPrint is one speaker's voice print in a recording.
type SpeakerPrint struct {
	Embedding []float64    `json:"embedding"`
	Seconds   float64      `json:"seconds"`
	Chunks    []VoiceChunk `json:"chunks"`
}

// VoicePrintResult is the voice print script's output.
type VoicePrintResult struct {
	Model    string                  `json:"model"`
	Dim      int                     `json:"dim"`
	Speakers map[string]SpeakerPrint `json:"speakers"`
}

func (p *PyAnnoteAdapter) copyVoicePrintScript() error {
	content, err := pyannoteScripts.ReadFile("py/pyannote/voiceprint.py")
	if err != nil {
		return fmt.Errorf("read embedded voiceprint.py: %w", err)
	}
	if err := os.MkdirAll(p.envPath, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.envPath, "voiceprint.py"), content, 0755)
}

// VoicePrints embeds each speaker's turns in an audio file with the PyAnnote
// environment (prepared first if needed). Script output goes to logW.
func (p *PyAnnoteAdapter) VoicePrints(ctx context.Context, audioPath string, turns []VoiceTurn, logW io.Writer) (*VoicePrintResult, error) {
	if err := p.PrepareEnvironment(ctx); err != nil {
		return nil, fmt.Errorf("prepare PyAnnote environment: %w", err)
	}
	if err := p.copyVoicePrintScript(); err != nil {
		return nil, err
	}
	token := os.Getenv("HF_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("HF_TOKEN is not set; voice prints use the PyAnnote models")
	}
	dir, err := os.MkdirTemp("", "scriberr-voice-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	in, out := filepath.Join(dir, "turns.json"), filepath.Join(dir, "prints.json")
	b, _ := json.Marshal(map[string]interface{}{"segments": turns})
	if err := os.WriteFile(in, b, 0600); err != nil {
		return nil, err
	}

	args := []string{"run", UVTLSFlag, "--project", p.envPath, "python",
		filepath.Join(p.envPath, "voiceprint.py"), audioPath, "--segments", in, "--output", out}
	cmd := exec.CommandContext(ctx, "uv", args...)
	cmd.Env = append(setEnv(os.Environ(), "HF_TOKEN", token), "PYTHONUNBUFFERED=1")
	var tail tailWriter
	if logW != nil {
		cmd.Stdout = io.MultiWriter(logW, &tail)
	} else {
		cmd.Stdout = &tail
	}
	cmd.Stderr = cmd.Stdout
	logger.Debug("Running voice print script", "audio", filepath.Base(audioPath), "turns", len(turns))
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("voice print script failed: %w: %s", err, strings.TrimSpace(tail.String()))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read voice prints: %w", err)
	}
	var res VoicePrintResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("parse voice prints: %w", err)
	}
	return &res, nil
}

// tailWriter keeps the last 2 KB written, for error messages.
type tailWriter struct{ buf []byte }

func (t *tailWriter) Write(b []byte) (int, error) {
	t.buf = append(t.buf, b...)
	if len(t.buf) > 2048 {
		t.buf = t.buf[len(t.buf)-2048:]
	}
	return len(b), nil
}

func (t *tailWriter) String() string { return string(t.buf) }
