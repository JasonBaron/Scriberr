package adapters

import (
	"io"
	"strings"

	"scriberr/internal/joblog"
)

// EnvLogTranscripts, when set to "true", keeps transcript text in job logs.
const EnvLogTranscripts = joblog.EnvLogTranscripts

// OpenJobLog opens <dir>/transcription.log for appending and returns a writer for
// a child process's stdout/stderr. Output is filtered (transcript text omitted,
// known-harmless warnings suppressed) and indented under the current job stage;
// see package joblog. Call the returned func when the process exits.
func OpenJobLog(dir string) (io.Writer, func(), error) {
	return joblog.OpenOutput(dir)
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
