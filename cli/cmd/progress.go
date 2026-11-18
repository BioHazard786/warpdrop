package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BioHazard786/Warpdrop/cli/internal/ui"
	"github.com/BioHazard786/Warpdrop/cli/internal/utils"
	"golang.org/x/term"
)

var isTTY = term.IsTerminal(int(os.Stdout.Fd()))

// meter prints one self-updating progress line. Updates come from transfer goroutines at chunk rate;
// the line is redrawn at 4 Hz. Without a terminal it prints nothing between start and finish.
type meter struct {
	mu          sync.Mutex
	label       string
	sent, total int64
	t0          time.Time
	lastT       time.Time
	lastB       int64
	speed       float64
	drawn       bool
}

func (m *meter) Update(label string, sent, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.t0.IsZero() || label != m.label {
		m.t0, m.lastT, m.lastB, m.speed = time.Now(), time.Now(), sent, 0
	}
	m.label, m.sent, m.total = label, sent, total
}

// Run redraws until ctx is done.
func (m *meter) Run(ctx context.Context) {
	if !isTTY {
		return
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			m.draw()
		case <-ctx.Done():
			return
		}
	}
}

func (m *meter) draw() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.label == "" {
		return
	}
	now := time.Now()
	if dt := now.Sub(m.lastT).Seconds(); dt >= 0.2 && m.sent >= m.lastB {
		inst := float64(m.sent-m.lastB) / dt
		if m.speed == 0 {
			m.speed = inst
		} else {
			m.speed = m.speed*0.7 + inst*0.3
		}
		m.lastT, m.lastB = now, m.sent
	}
	pct := 0.0
	if m.total > 0 {
		pct = float64(m.sent) / float64(m.total)
	}
	const w = 24
	filled := int(pct * w)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
	eta := ""
	if m.speed > 1 && m.total > m.sent {
		eta = " ETA " + utils.FormatTimeDuration(time.Duration(float64(m.total-m.sent)/m.speed*float64(time.Second)))
	}
	fmt.Printf("\r\033[K  %s %s %5.1f%%  %s%s", utils.TruncateString(m.label, 24), bar, pct*100,
		ui.MutedStyle.Render(utils.FormatSpeed(m.speed)), ui.MutedStyle.Render(eta))
	m.drawn = true
}

// Clear erases the progress line so normal output can follow it.
func (m *meter) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.drawn {
		fmt.Print("\r\033[K")
		m.drawn = false
	}
	m.label = ""
}
