// Package clock keeps stored timestamps in UTC while showing log times in the
// operator's time zone.
//
// SQLite stores times as text with their UTC offset and sorts them as text.
// When TZ (e.g. America/New_York) applied to the whole process, new rows were
// written as "11:57-04:00" next to older "14:36+00:00" rows, so the newest jobs
// sorted below older ones. The server therefore runs with time.Local = UTC for
// data, and formats human-facing log times with Display.
package clock

import "time"

// Display is the zone from TZ, captured before UseUTCForData switches
// time.Local to UTC. Package variables initialise before main runs.
var Display = time.Local

// UseUTCForData makes time.Now() and everything derived from time.Local use
// UTC, so stored timestamps stay consistent. Call first thing in main.
func UseUTCForData() {
	time.Local = time.UTC
}

// Now returns the current time in the display zone (for logs only).
func Now() time.Time { return time.Now().In(Display) }
