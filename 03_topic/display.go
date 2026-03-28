package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

// LiveStats prints scenario progress in-place using ANSI cursor control.
// Call Start before the scenario loop, Update periodically, and Done when finished.
// lineOffset > 0 reserves that many lines above the cursor so parallel instances
// don't overwrite each other: offset 0 = current line, 1 = one line above, etc.
type LiveStats struct {
	name       string
	target     int64
	counter    *atomic.Int64
	tli        *atomic.Int64
	start      time.Time
	stopCh     chan struct{}
	doneCh     chan struct{}
	lineOffset int
}

// NewLiveStats creates a LiveStats for a scenario. tliCounter may be nil.
func NewLiveStats(name string, target int64, counter *atomic.Int64, tliCounter *atomic.Int64) *LiveStats {
	return NewLiveStatsAt(name, target, counter, tliCounter, 0)
}

// NewLiveStatsAt is like NewLiveStats but reserves lineOffset lines above the cursor.
func NewLiveStatsAt(name string, target int64, counter *atomic.Int64, tliCounter *atomic.Int64, lineOffset int) *LiveStats {
	return &LiveStats{
		name:       name,
		target:     target,
		counter:    counter,
		tli:        tliCounter,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		lineOffset: lineOffset,
	}
}

// Start begins printing live stats in the background every 250ms.
func (s *LiveStats) Start() {
	s.start = time.Now()
	s.printLine() // initial line
	go func() {
		defer close(s.doneCh)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.printLine()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop halts background printing and prints the final stats line.
func (s *LiveStats) Stop() {
	close(s.stopCh)
	<-s.doneCh
	s.printLine() // final snapshot
	fmt.Println() // leave cursor on a new line
}

func (s *LiveStats) printLine() {
	processed := s.counter.Load()
	elapsed := time.Since(s.start)

	var msgPerSec float64
	if elapsed.Seconds() > 0 {
		msgPerSec = float64(processed) / elapsed.Seconds()
	}

	var tliVal int64
	if s.tli != nil {
		tliVal = s.tli.Load()
	}

	pct := float64(processed) / float64(s.target) * 100
	if pct > 100 {
		pct = 100
	}

	line := fmt.Sprintf(
		"%-32s  %7d/%d (%5.1f%%)  %8.0f msg/s  TLI: %d  %s",
		s.name, processed, s.target, pct, msgPerSec, tliVal, formatDuration(elapsed),
	)

	// Move cursor up lineOffset lines, go to beginning, overwrite, return to original row.
	if s.lineOffset > 0 {
		fmt.Printf("\033[%dA\r%-*s\033[%dB", s.lineOffset, len(line)+4, line, s.lineOffset)
	} else {
		fmt.Printf("\r%-*s", len(line)+4, line)
	}
}
