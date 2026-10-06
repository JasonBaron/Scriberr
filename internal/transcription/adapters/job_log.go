package adapters

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// EnvLogTranscripts, when set to "true", keeps transcript text in job logs
// (the previous behaviour). Default is to omit it.
const EnvLogTranscripts = "SCRIBERR_LOG_TRANSCRIPTS"

// transcriptLinePrefixes are line starts that carry recognised speech text.
//   - "Transcript:"     WhisperX --verbose segment output
//   - "Transcription: " Parakeet full-text output ("Transcription complete:" is kept)
//   - "Result: "        Canary full-text output
var transcriptLinePrefixes = []string{"Transcript:", "Transcription: ", "Result: "}

// redactingWriter drops lines containing transcript text before they reach the
// underlying writer. Lines end at '\n' or '\r' (progress bars use '\r'), so the
// log still updates live while a job runs.
type redactingWriter struct {
	mu      sync.Mutex
	dst     io.Writer
	buf     []byte
	omitted int
}

func newRedactingWriter(dst io.Writer) *redactingWriter {
	return &redactingWriter{dst: dst}
}

func isTranscriptLine(line []byte) bool {
	s := strings.TrimLeft(string(line), " \t")
	for _, p := range transcriptLinePrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Write buffers input and forwards complete, non-transcript lines.
// It always reports len(p) so the child process never sees a short write.
func (w *redactingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		line, term := w.buf[:i], w.buf[i:i+1]
		if isTranscriptLine(line) {
			w.omitted++
		} else if _, err := w.dst.Write(append(append([]byte{}, line...), term...)); err != nil {
			return len(p), err
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

// Flush writes any trailing partial line and a one-line note if anything was omitted.
func (w *redactingWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		if isTranscriptLine(w.buf) {
			w.omitted++
		} else if _, err := w.dst.Write(append(w.buf, '\n')); err != nil {
			return err
		}
		w.buf = nil
	}
	if w.omitted > 0 {
		_, err := fmt.Fprintf(w.dst, "[scriberr] %d transcript line(s) omitted from this log (set %s=true to keep them)\n", w.omitted, EnvLogTranscripts)
		w.omitted = 0
		return err
	}
	return nil
}

// OpenJobLog opens <dir>/transcription.log for appending and returns a writer for
// a child process's stdout/stderr. Transcript text is omitted unless
// SCRIBERR_LOG_TRANSCRIPTS=true. Call the returned func when the process exits.
func OpenJobLog(dir string) (io.Writer, func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, "transcription.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, err
	}
	if strings.EqualFold(os.Getenv(EnvLogTranscripts), "true") {
		return f, func() { _ = f.Close() }, nil
	}
	rw := newRedactingWriter(f)
	return rw, func() { _ = rw.Flush(); _ = f.Close() }, nil
}

// setEnv returns env with key set to value, replacing any existing entry.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, prefix+value)
}
