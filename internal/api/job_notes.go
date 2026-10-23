package api

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"scriberr/pkg/clock"
)

var jobNoteMu sync.Mutex

// jobNote appends a timestamped line to a recording's job log
// (transcription.log), so summaries, tags and voice scans show up next to
// the transcription that produced them. Errors are ignored: the log is a
// convenience.
func (h *Handler) jobNote(jobID, format string, args ...interface{}) {
	path := h.jobLogPath(jobID)
	if path == "" {
		return
	}
	line := time.Now().In(clock.Display).Format("2006-01-02 15:04:05") + "  " + strings.TrimRight(fmt.Sprintf(format, args...), "\n") + "\n"
	jobNoteMu.Lock()
	defer jobNoteMu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}
