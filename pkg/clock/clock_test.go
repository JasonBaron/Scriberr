package clock

import (
	"testing"
	"time"
)

func TestUseUTCForDataKeepsDisplayZone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no zone data:", err)
	}
	origLocal, origDisplay := time.Local, Display
	defer func() { time.Local, Display = origLocal, origDisplay }()

	time.Local = ny
	Display = time.Local // what package init captures when TZ=America/New_York
	UseUTCForData()

	if name, off := time.Now().Zone(); off != 0 {
		t.Errorf("time.Now() zone = %s %d, want UTC for stored data", name, off)
	}
	ts := time.Date(2026, 10, 8, 15, 57, 0, 0, time.UTC)
	if got := ts.In(Display).Format("15:04 MST"); got != "11:57 EDT" {
		t.Errorf("display time = %q, want 11:57 EDT", got)
	}
}
