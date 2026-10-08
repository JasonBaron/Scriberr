// Package mediainfo reads recording metadata from media files.
package mediainfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"scriberr/pkg/clock"
)

// Source says where a recorded time came from.
const (
	SourceMetadata = "metadata" // tag embedded in the file
	SourceFileTime = "file"     // the file's modified time, sent by the browser or CLI
)

// Tag names that hold a recording time, most specific first. Matched
// case-insensitively against format and stream tags from ffprobe.
var timeTags = []string{
	"com.apple.quicktime.creationdate", // iPhone Voice Memos, QuickTime
	"creation_time",                    // MP4/M4A/MOV, many recorders
	"date_recorded",
	"originationdate", // BWF bext chunk (with originationtime)
	"origination_date",
	"date", // WAV LIST/ICRD, ID3 TDRC
	"icrd",
}

// Layouts with a zone or a Z suffix.
var zonedLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05-0700",
}

// Layouts without a zone: recorders write these in local time.
var localLayouts = []string{
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006:01:02 15:04:05",
}

// Date-only layouts: kept at noon local time so the date survives any
// conversion to and from UTC.
var dateLayouts = []string{
	"2006-01-02",
	"2006:01:02",
}

// earliest rejects placeholder dates (1904 and 1970 epochs from recorders
// whose clock was never set).
var earliest = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)

type probe struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Tags map[string]string `json:"tags"`
	} `json:"streams"`
}

// RecordedAt returns the recording time embedded in the file, if any.
func RecordedAt(ctx context.Context, path string) (time.Time, bool) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "quiet", "-print_format", "json",
		"-show_entries", "format_tags:stream_tags", path).Output()
	if err != nil {
		return time.Time{}, false
	}
	return FromProbeJSON(out, time.Now(), clock.Display)
}

// FromProbeJSON picks the recording time from ffprobe JSON output. now
// bounds the result: anything more than a day in the future is ignored.
// loc is the zone for tags written without one.
func FromProbeJSON(raw []byte, now time.Time, loc *time.Location) (time.Time, bool) {
	var p probe
	if err := json.Unmarshal(raw, &p); err != nil {
		return time.Time{}, false
	}
	tags := map[string]string{}
	add := func(m map[string]string) {
		for k, v := range m {
			k = strings.ToLower(strings.TrimSpace(k))
			if _, seen := tags[k]; !seen && strings.TrimSpace(v) != "" {
				tags[k] = strings.TrimSpace(v)
			}
		}
	}
	add(p.Format.Tags)
	for _, s := range p.Streams {
		add(s.Tags)
	}

	for _, name := range timeTags {
		v, ok := tags[name]
		if !ok {
			continue
		}
		if name == "originationdate" || name == "origination_date" {
			for _, tn := range []string{"originationtime", "origination_time"} {
				if t, ok := tags[tn]; ok {
					v = v + " " + strings.ReplaceAll(t, "-", ":")
					break
				}
			}
		}
		if t, ok := Parse(v, loc); ok && valid(t, now) {
			return t, true
		}
	}
	return time.Time{}, false
}

// Parse reads a metadata date in any of the common layouts. Times without
// a zone are taken in loc; dates without a time are set to noon in loc.
func Parse(v string, loc *time.Location) (time.Time, bool) {
	v = strings.TrimSpace(v)
	for _, l := range zonedLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.UTC(), true
		}
	}
	for _, l := range localLayouts {
		if t, err := time.ParseInLocation(l, v, loc); err == nil {
			return t.UTC(), true
		}
	}
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, v, loc); err == nil {
			return t.Add(12 * time.Hour).UTC(), true
		}
	}
	return time.Time{}, false
}

// FromUnixMillis converts a browser File.lastModified value.
func FromUnixMillis(ms int64, now time.Time) (time.Time, bool) {
	if ms <= 0 {
		return time.Time{}, false
	}
	t := time.UnixMilli(ms).UTC()
	return t, valid(t, now)
}

func valid(t, now time.Time) bool {
	return !t.Before(earliest) && !t.After(now.Add(24*time.Hour))
}
