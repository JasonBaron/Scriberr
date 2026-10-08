// Package joblog writes the structured, human-readable per-job log that the
// View Logs API serves: a header, pre-flight checks, numbered stages with
// timings, filtered model output, and a SUCCESS/FAILED footer.
//
// It depends only on the standard library so adapters and the pipeline can
// both use it. Every method is safe on a nil *Log and never fails the job:
// logging problems are swallowed.
package joblog

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Version is stamped at build time:
//
//	go build -ldflags "-X scriberr/internal/joblog.Version=$(git rev-parse --short HEAD)"
var Version = "dev"

const (
	bar  = "======================================"
	rule = "--------------------------------------"
)

// Log is one job run's structured log.
type Log struct {
	mu         sync.Mutex
	dir        string
	path       string
	jobID      string
	start      time.Time
	audioSec   float64
	total      int
	stageNum   int
	stageName  string
	stageStart time.Time
	stageOpen  bool
	now        func() time.Time
}

// Open starts a new run in <dir>/transcription.log (appending, so earlier
// runs of the same job stay above it). It returns nil if the directory
// cannot be created; all methods are no-ops on nil.
func Open(dir, jobID string) *Log {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil
	}
	takeOutcome(dir) // clear anything left from a previous run
	return &Log{
		dir:   dir,
		path:  filepath.Join(dir, LogFileName),
		jobID: jobID,
		start: time.Now(),
		now:   time.Now,
	}
}

func (l *Log) write(s string) {
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(s)
	_ = f.Close()
}

func field(key, val string) string { return fmt.Sprintf("%-12s: %s\n", key, val) }

// Field is one "Key : value" line.
type Field struct{ Key, Value string }

// Header writes the run banner. Empty values are skipped.
func (l *Log) Header(fields ...Field) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	b.WriteString("\n" + bar + "\nSCRIBERR JOB START\n" + bar + "\n")
	b.WriteString(field("Job ID", l.jobID))
	for _, f := range fields {
		if f.Value != "" {
			b.WriteString(field(f.Key, f.Value))
		}
	}
	b.WriteString(field("Started", l.start.Format("2006-01-02 15:04:05 MST")))
	b.WriteString(field("Version", Version))
	b.WriteString(bar + "\n")
	l.write(b.String())
}

// Section writes a titled block of fields, e.g. PRE-FLIGHT.
func (l *Log) Section(title string, fields ...Field) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var b strings.Builder
	b.WriteString(title + "\n" + rule + "\n")
	for _, f := range fields {
		if f.Value != "" {
			b.WriteString(field(f.Key, f.Value))
		}
	}
	l.write(b.String())
}

// SetAudioSeconds records the audio length so the footer can report RTF.
func (l *Log) SetAudioSeconds(sec float64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.audioSec = sec
	l.mu.Unlock()
}

// SetTotalStages sets N in the "[i/N]" stage counter.
func (l *Log) SetTotalStages(n int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.total = n
	l.mu.Unlock()
}

// Stage starts the next numbered stage. Model output written while it runs
// appears indented beneath it. Close it with Done; if the job fails first,
// Finish marks it FAILED.
func (l *Log) Stage(name string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stageOpen {
		l.write(fmt.Sprintf("      done  %s\n", fmtDur(l.now().Sub(l.stageStart))))
	}
	l.stageNum++
	if l.total < l.stageNum {
		l.total = l.stageNum
	}
	l.stageName = name
	l.stageStart = l.now()
	l.stageOpen = true
	l.write(fmt.Sprintf("[%d/%d] %s...\n", l.stageNum, l.total, name))
}

// Done closes the current stage with its duration and an optional note.
func (l *Log) Done(note string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.stageOpen {
		return
	}
	l.stageOpen = false
	s := fmt.Sprintf("      done  %s", fmtDur(l.now().Sub(l.stageStart)))
	if note != "" {
		s += "  (" + note + ")"
	}
	l.write(s + "\n")
}

// Note writes an indented informational line under the current stage.
func (l *Log) Note(format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.write("      " + fmt.Sprintf(format, args...) + "\n")
}

// Finish writes the footer. err == nil means success.
func (l *Log) Finish(err error) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	elapsed := l.now().Sub(l.start)
	lastErr, hintList := takeOutcome(l.dir)

	var b strings.Builder
	if err != nil && l.stageOpen {
		b.WriteString(fmt.Sprintf("      FAILED %s\n", fmtDur(l.now().Sub(l.stageStart))))
	} else if l.stageOpen {
		b.WriteString(fmt.Sprintf("      done  %s\n", fmtDur(l.now().Sub(l.stageStart))))
	}
	failedStage := l.stageOpen && err != nil
	l.stageOpen = false

	status := "SUCCESS"
	title := "SCRIBERR JOB COMPLETE"
	if err != nil {
		status = "FAILED"
		title = "SCRIBERR JOB FAILED"
	}
	b.WriteString(bar + "\n" + title + "\n" + bar + "\n")
	b.WriteString(field("Job ID", l.jobID))
	if err != nil {
		if failedStage {
			b.WriteString(field("Failed at", fmt.Sprintf("[%d/%d] %s", l.stageNum, l.total, l.stageName)))
		}
		msg := lastErr
		if msg == "" {
			msg = firstLine(err.Error())
		}
		b.WriteString(field("Error", msg))
		for _, h := range hintList {
			b.WriteString(field("Hint", h))
		}
	}
	dur := fmt.Sprintf("%.1f s", elapsed.Seconds())
	if l.audioSec > 0 {
		dur += fmt.Sprintf(" (RTF %.3f)", elapsed.Seconds()/l.audioSec)
	}
	b.WriteString(field("Duration", dur))
	b.WriteString(field("Status", status))
	b.WriteString(bar + "\n")
	l.write(b.String())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

func fmtDur(d time.Duration) string { return fmt.Sprintf("%.1fs", d.Seconds()) }

// runQuick runs a short command with a timeout and returns trimmed stdout.
func runQuick(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// GPUInfo returns e.g. "NVIDIA GeForce RTX 3060, 11.2 GB free of 12.0 GB",
// or "not detected".
func GPUInfo() string {
	out := runQuick(3*time.Second, "nvidia-smi", "--query-gpu=name,memory.free,memory.total", "--format=csv,noheader,nounits")
	if out == "" {
		return "not detected"
	}
	var parts []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ",")
		if len(f) != 3 {
			parts = append(parts, strings.TrimSpace(line))
			continue
		}
		var free, total float64
		_, _ = fmt.Sscanf(strings.TrimSpace(f[1]), "%f", &free)
		_, _ = fmt.Sscanf(strings.TrimSpace(f[2]), "%f", &total)
		parts = append(parts, fmt.Sprintf("%s, %.1f GB free of %.1f GB", strings.TrimSpace(f[0]), free/1024, total/1024))
	}
	return strings.Join(parts, "; ")
}

// UVVersion returns e.g. "uv 0.9.18 (...)" or "not found".
func UVVersion() string {
	if out := runQuick(3*time.Second, "uv", "--version"); out != "" {
		return out
	}
	return "not found"
}
