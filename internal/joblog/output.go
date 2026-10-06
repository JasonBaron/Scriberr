package joblog

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// EnvLogTranscripts, when "true", keeps transcript text in job logs.
	EnvLogTranscripts = "SCRIBERR_LOG_TRANSCRIPTS"
	// EnvLogVerbose, when "true", writes raw model output (no noise filtering,
	// no indentation). Transcript text is still governed by EnvLogTranscripts.
	EnvLogVerbose = "SCRIBERR_LOG_VERBOSE"

	// LogFileName is the per-job log file read by the View Logs API.
	LogFileName = "transcription.log"
)

func envTrue(key string) bool { return strings.EqualFold(os.Getenv(key), "true") }

// transcriptLinePrefixes are line starts that carry recognised speech text.
//   - "Transcript:"     WhisperX --verbose segment output
//   - "Transcription: " Parakeet full-text output ("Transcription complete:" is kept)
//   - "Result: "        Canary full-text output
var transcriptLinePrefixes = []string{"Transcript:", "Transcription: ", "Result: "}

func isTranscriptLine(line string) bool {
	s := strings.TrimLeft(line, " \t")
	for _, p := range transcriptLinePrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// noiseRule matches a known-harmless output line. Rules with a kind are the
// first line of a warning and are counted; rules without one are continuation
// lines of a multi-line warning and are dropped silently.
type noiseRule struct {
	re   *regexp.Regexp
	kind string
}

var noiseRules = []noiseRule{
	{regexp.MustCompile("`--native-tls` flag is deprecated"), "uv native-tls"},
	{regexp.MustCompile(`Lightning automatically upgraded your loaded checkpoint`), "lightning checkpoint"},
	{regexp.MustCompile(`ReproducibilityWarning: TensorFloat-32`), "TF32"},
	{regexp.MustCompile(`No --hf_token provided`), "hf_token flag"},
	{regexp.MustCompile(`UserWarning: std\(\): degrees of freedom`), "pyannote std()"},
	// continuation lines
	{regexp.MustCompile(`^\s*It can be re-enabled by calling\s*$`), ""},
	{regexp.MustCompile(`^\s*>>> `), ""},
	{regexp.MustCompile(`pyannote-audio/issues/1370`), ""},
	{regexp.MustCompile(`^\s*warnings\.warn\($`), ""},
	{regexp.MustCompile(`^\s*std = sequences\.std\(`), ""},
	{regexp.MustCompile(`^\s*$`), ""},
	// progress bars (tqdm and similar)
	{regexp.MustCompile(`\d+%\|`), ""},
	{regexp.MustCompile(`(it/s|s/it)\]`), ""},
}

func matchNoise(line string) (bool, string) {
	for _, r := range noiseRules {
		if r.re.MatchString(line) {
			return true, r.kind
		}
	}
	return false, ""
}

// pyLogPrefix matches Python logging lines such as
// "2026-10-06 19:47:47 - whisperx.transcribe - INFO - Performing transcription..."
var pyLogPrefix = regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(?:,\d+)? - [\w.]+ - (DEBUG|INFO|WARNING|ERROR|CRITICAL) - (.*)$`)

// stepRe matches WhisperX sub-step announcements ("Performing alignment...").
var stepRe = regexp.MustCompile(`^Performing (.+?)\.\.\.$`)

// errorRe flags lines worth surfacing in the failure footer.
var errorRe = regexp.MustCompile(`(?i)(^error\b|error:|error during|exception\b|^traceback|out of memory|is not defined|exit status [1-9])`)
var warnRe = regexp.MustCompile(`(?i)warn`)

// hint maps substrings seen anywhere in model output to an actionable hint.
type hint struct {
	needles []string
	text    string
}

var hints = []hint{
	{[]string{"libnvrtc.so.13", "AudioDecoder' is not defined", "torchcodec is not installed correctly"},
		"torchcodec failed to load. The env's torchcodec must match torch (0.7.x for torch 2.8); a restart re-syncs pinned envs"},
	{[]string{"CUDA out of memory", "OutOfMemoryError"},
		"GPU ran out of memory. Lower batch size, use float16/int8, or check for other GPU jobs (QUEUE_WORKERS=1)"},
	{[]string{"Hugging Face token missing", "GatedRepoError", "gated repo", "401 Client Error"},
		"Hugging Face token missing or lacks access to the pyannote models. Check HF_TOKEN and accept the model terms on huggingface.co"},
	{[]string{"Could not load library libcudnn", "libcudnn"},
		"cuDNN library not found. Check LD_LIBRARY_PATH points at the venv's nvidia/cudnn libs"},
}

// outcome collects what the output filters saw for one job directory, so the
// job footer can report the real error instead of a wrapped Go error.
type outcome struct {
	lastError string
	hints     map[string]bool
}

var (
	outcomesMu sync.Mutex
	outcomes   = map[string]*outcome{}
)

func outcomeKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

func recordOutcome(dir, line string, isErr bool) {
	var matched []string
	for _, h := range hints {
		for _, n := range h.needles {
			if strings.Contains(line, n) {
				matched = append(matched, h.text)
				break
			}
		}
	}
	if !isErr && len(matched) == 0 {
		return
	}
	outcomesMu.Lock()
	defer outcomesMu.Unlock()
	k := outcomeKey(dir)
	o := outcomes[k]
	if o == nil {
		o = &outcome{hints: map[string]bool{}}
		outcomes[k] = o
	}
	if isErr {
		s := strings.TrimSpace(line)
		if len(s) > 300 {
			s = s[:300] + "..."
		}
		o.lastError = s
	}
	for _, m := range matched {
		o.hints[m] = true
	}
}

// takeOutcome returns and clears what was recorded for dir.
func takeOutcome(dir string) (lastError string, hintList []string) {
	outcomesMu.Lock()
	defer outcomesMu.Unlock()
	k := outcomeKey(dir)
	o := outcomes[k]
	delete(outcomes, k)
	if o == nil {
		return "", nil
	}
	for h := range o.hints {
		hintList = append(hintList, h)
	}
	sort.Strings(hintList)
	return o.lastError, hintList
}

// outputWriter filters a child process's stdout/stderr on its way into the
// job log: transcript text is omitted, known-harmless warnings and progress
// bars are dropped (and counted), Python log prefixes are trimmed, WhisperX
// sub-steps get an elapsed-time marker, and every kept line is indented under
// the current stage. Lines end at '\n' or '\r'. Writes always report len(p) so
// the child never sees a short write.
type outputWriter struct {
	mu         sync.Mutex
	dst        io.Writer
	dir        string
	buf        []byte
	start      time.Time
	keepText   bool
	verbose    bool
	omitted    int
	suppressed map[string]int
	now        func() time.Time
}

func newOutputWriter(dst io.Writer, dir string, keepText, verbose bool) *outputWriter {
	return &outputWriter{
		dst:        dst,
		dir:        dir,
		start:      time.Now(),
		keepText:   keepText,
		verbose:    verbose,
		suppressed: map[string]int{},
		now:        time.Now,
	}
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		line, term := string(w.buf[:i]), w.buf[i]
		w.buf = w.buf[i+1:]
		if err := w.handle(line, term); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (w *outputWriter) handle(line string, term byte) error {
	if !w.keepText && isTranscriptLine(line) {
		w.omitted++
		return nil
	}

	if w.verbose {
		recordOutcome(w.dir, line, errorRe.MatchString(line) && !warnRe.MatchString(line))
		_, err := w.dst.Write(append([]byte(line), term))
		return err
	}

	if noisy, kind := matchNoise(line); noisy {
		if kind != "" {
			w.suppressed[kind]++
		}
		return nil
	}

	msg := strings.TrimRight(line, " \t")
	if m := pyLogPrefix.FindStringSubmatch(msg); m != nil {
		if m[1] == "INFO" || m[1] == "DEBUG" {
			msg = m[2]
		} else {
			msg = m[1] + ": " + m[2]
		}
	}
	recordOutcome(w.dir, msg, errorRe.MatchString(msg) && !warnRe.MatchString(msg))

	if s := stepRe.FindStringSubmatch(msg); s != nil {
		msg = fmt.Sprintf("- %s (+%.1fs)", s[1], w.now().Sub(w.start).Seconds())
	}
	_, err := fmt.Fprintf(w.dst, "      %s\n", msg)
	return err
}

// Flush writes any trailing partial line and a summary of what was omitted.
func (w *outputWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) > 0 {
		line := string(w.buf)
		w.buf = nil
		if err := w.handle(line, '\n'); err != nil {
			return err
		}
	}
	if w.omitted > 0 {
		if _, err := fmt.Fprintf(w.dst, "      [scriberr] %d transcript line(s) omitted from this log (set %s=true to keep them)\n", w.omitted, EnvLogTranscripts); err != nil {
			return err
		}
		w.omitted = 0
	}
	if n, kinds := w.suppressedSummary(); n > 0 {
		if _, err := fmt.Fprintf(w.dst, "      [scriberr] %d known warning(s) suppressed (%s); set %s=true for raw output\n", n, kinds, EnvLogVerbose); err != nil {
			return err
		}
		w.suppressed = map[string]int{}
	}
	return nil
}

func (w *outputWriter) suppressedSummary() (int, string) {
	total := 0
	kinds := make([]string, 0, len(w.suppressed))
	for k, n := range w.suppressed {
		total += n
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return total, strings.Join(kinds, ", ")
}

// OpenOutput opens <dir>/transcription.log for appending and returns a writer
// for a child process's stdout/stderr, plus a func to call when it exits.
func OpenOutput(dir string) (io.Writer, func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, LogFileName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, err
	}
	w := newOutputWriter(f, dir, envTrue(EnvLogTranscripts), envTrue(EnvLogVerbose))
	return w, func() { _ = w.Flush(); _ = f.Close() }, nil
}
