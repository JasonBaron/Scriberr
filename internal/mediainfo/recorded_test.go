package mediainfo

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 8, 19, 0, 0, 0, time.UTC)

var ny, _ = time.LoadLocation("America/New_York")

func TestFromProbeJSON(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string // RFC3339, "" for none
	}{
		{"m4a creation_time", `{"format":{"tags":{"creation_time":"2026-09-14T13:05:22.000000Z"}}}`, "2026-09-14T13:05:22Z"},
		{"apple creationdate wins", `{"format":{"tags":{"creation_time":"2026-09-14T17:05:22.000000Z","com.apple.quicktime.creationdate":"2026-09-14T13:05:22-0400"}}}`, "2026-09-14T17:05:22Z"},
		{"stream tag", `{"format":{},"streams":[{"tags":{"creation_time":"2025-01-02T03:04:05Z"}}]}`, "2025-01-02T03:04:05Z"},
		{"wav icrd date only keeps the date", `{"format":{"tags":{"date":"2024-05-06"}}}`, "2024-05-06T16:00:00Z"},
		{"bwf origination is local time", `{"format":{"tags":{"origination_date":"2023-07-08","origination_time":"09-10-11"}}}`, "2023-07-08T13:10:11Z"},
		{"1904 placeholder", `{"format":{"tags":{"creation_time":"1904-01-01T00:00:00.000000Z"}}}`, ""},
		{"future", `{"format":{"tags":{"creation_time":"2030-01-01T00:00:00Z"}}}`, ""},
		{"no tags", `{"format":{}}`, ""},
		{"garbage", `not json`, ""},
	}
	for _, c := range cases {
		got, ok := FromProbeJSON([]byte(c.json), now, ny)
		if c.want == "" {
			if ok {
				t.Errorf("%s: want none, got %s", c.name, got)
			}
			continue
		}
		if !ok || got.Format(time.RFC3339) != c.want {
			t.Errorf("%s: want %s, got %s (ok=%v)", c.name, c.want, got.Format(time.RFC3339), ok)
		}
	}
}

func TestFromUnixMillis(t *testing.T) {
	if _, ok := FromUnixMillis(0, now); ok {
		t.Error("zero should be rejected")
	}
	got, ok := FromUnixMillis(1757855122000, now)
	if !ok || got.Year() != 2025 {
		t.Errorf("got %s ok=%v", got, ok)
	}
}
