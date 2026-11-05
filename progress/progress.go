// Package progress prints live status while a simulation runs.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Snapshot is one moment of a running simulation for display.
type Snapshot struct {
	Now       int64 // simulated clock
	StartAt   int64 // first requested_at
	HorizonAt int64 // scenario end used for the percent bar

	DriversOnline        int
	DriversIdle          int
	DriversEnRoute       int
	DriversOnTrip        int
	DriversRepositioning int

	RequestsWaiting   int // status requested
	RequestsAssigned  int // assigned, driver_arrived or on_trip
	RequestsCompleted int
	RequestsCanceled  int
	RequestsTotal     int

	// OSRM is filled only when the OSRM router is in use.
	OSRM            bool
	OSRMTable       int64 // /table HTTP requests
	OSRMRoute       int64 // /route HTTP requests
	OSRMCacheHits   int64 // pairs served without a new HTTP request
	OSRMCacheMisses int64 // pairs that needed a new HTTP request
}

// Reporter writes Snapshots to a writer, redrawing in place on a terminal
// and printing a plain line every few seconds otherwise.
type Reporter struct {
	Writer     io.Writer
	Terminal   bool
	Location   *time.Location // nil means show only the T+ offset
	Interval   time.Duration  // minimum time between writes; 0 picks a default
	started    time.Time
	lastWrite  time.Time
	lastLength int
	done       bool
}

// IsTerminal reports whether w is an interactive terminal.
func IsTerminal(w *os.File) bool {
	if w == nil {
		return false
	}
	info, err := w.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (r *Reporter) interval() time.Duration {
	if r.Interval > 0 {
		return r.Interval
	}
	if r.Terminal {
		return 250 * time.Millisecond
	}
	return 2 * time.Second
}

// Update writes a status line when enough wall time has passed since the
// last write. It never changes simulation state.
func (r *Reporter) Update(snap Snapshot) {
	if r == nil || r.Writer == nil || r.done {
		return
	}
	now := time.Now()
	if r.started.IsZero() {
		r.started = now
	}
	if !r.lastWrite.IsZero() && now.Sub(r.lastWrite) < r.interval() {
		return
	}
	r.write(snap, now)
}

// Force writes a status line immediately, ignoring the interval. Use it for
// the final snapshot so the last counts are always shown.
func (r *Reporter) Force(snap Snapshot) {
	if r == nil || r.Writer == nil || r.done {
		return
	}
	now := time.Now()
	if r.started.IsZero() {
		r.started = now
	}
	r.write(snap, now)
}

// Finish ends the status line. On a terminal it writes a trailing newline
// so the next stderr message starts on its own row. Call it after the last
// Force or Update so the final counts stay visible.
func (r *Reporter) Finish() {
	if r == nil || r.Writer == nil || r.done {
		return
	}
	if r.Terminal && r.lastLength > 0 {
		fmt.Fprintln(r.Writer)
	}
	r.done = true
}

func (r *Reporter) write(snap Snapshot, wall time.Time) {
	elapsed := wall.Sub(r.started)
	line := Format(snap, elapsed, r.Location)
	if r.Terminal {
		// Clear the previous line, then rewrite in place.
		clear := ""
		if r.lastLength > len(line) {
			clear = strings.Repeat(" ", r.lastLength-len(line))
		}
		fmt.Fprintf(r.Writer, "\r%s%s\r%s", line, clear, line)
		r.lastLength = len(line)
	} else {
		fmt.Fprintln(r.Writer, line)
		r.lastLength = 0
	}
	r.lastWrite = wall
}

// Format builds one status line. It is a pure function so tests can assert
// on the text without a running simulation.
func Format(snap Snapshot, elapsed time.Duration, location *time.Location) string {
	percent := 0.0
	if snap.HorizonAt > snap.StartAt {
		percent = 100 * float64(snap.Now-snap.StartAt) / float64(snap.HorizonAt-snap.StartAt)
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
	} else if snap.HorizonAt > 0 && snap.Now >= snap.HorizonAt {
		percent = 100
	}
	remaining := ""
	if percent > 0.5 && percent < 100 && elapsed > 0 {
		total := time.Duration(float64(elapsed) * 100 / percent)
		left := total - elapsed
		if left < 0 {
			left = 0
		}
		remaining = "  remaining " + formatDuration(left)
	} else if percent >= 100 {
		remaining = "  remaining 0s"
	}

	var b strings.Builder
	b.WriteString(bar(percent))
	fmt.Fprintf(&b, " %5.1f%%", percent)
	b.WriteString("  ")
	b.WriteString(formatSimulatedClock(snap.Now, snap.StartAt, location))
	b.WriteString("  elapsed ")
	b.WriteString(formatDuration(elapsed))
	b.WriteString(remaining)
	fmt.Fprintf(&b, "  |  drivers %d online (%d idle, %d en route, %d on trip, %d repositioning)",
		snap.DriversOnline, snap.DriversIdle, snap.DriversEnRoute, snap.DriversOnTrip, snap.DriversRepositioning)
	fmt.Fprintf(&b, "  |  requests %d waiting, %d assigned, %d completed, %d canceled",
		snap.RequestsWaiting, snap.RequestsAssigned, snap.RequestsCompleted, snap.RequestsCanceled)
	if snap.RequestsTotal > 0 {
		fmt.Fprintf(&b, " / %d", snap.RequestsTotal)
	}
	if snap.OSRM {
		fmt.Fprintf(&b, "  |  osrm %d table, %d route, %d cache hits",
			snap.OSRMTable, snap.OSRMRoute, snap.OSRMCacheHits)
		if lookups := snap.OSRMCacheHits + snap.OSRMCacheMisses; lookups > 0 {
			fmt.Fprintf(&b, " (%.1f%% hit rate)", 100*float64(snap.OSRMCacheHits)/float64(lookups))
		}
	}
	return b.String()
}

func bar(percent float64) string {
	const width = 20
	filled := int(percent / 100 * width)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return "[" + strings.Repeat("=", filled) + strings.Repeat("-", width-filled) + "]"
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes = minutes % 60
	return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
}

func formatSimulatedClock(now, startAt int64, location *time.Location) string {
	offset := now - startAt
	var text string
	if offset < 0 {
		text = fmt.Sprintf("T%d", offset) // before the first request, e.g. T-120
	} else {
		text = fmt.Sprintf("T+%d", offset)
	}
	if location == nil {
		return text
	}
	local := time.Unix(now, 0).In(location)
	return fmt.Sprintf("%s · %s", text, local.Format("3:04:05 PM MST"))
}
