// Package autosplit watches one career profile and splits in LiveSplit each
// time the game saves a newly won map in it. It only ever reads that file:
// no access to the game's process or memory.
package autosplit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"todhelper/internal/czp"
	"todhelper/internal/game"
)

type Event struct {
	ID   int64     `json:"id"`
	Time time.Time `json:"time"`
	Kind string    `json:"kind"` // split, save, reset, info, warn, error
	Text string    `json:"text"`
}

// Timer is what the watcher splits: LiveSplit (TCP server) or LiveSplit One
// (WebSocket "Connect to Server").
type Timer interface {
	Name() string
	Describe() string // address or URL
	Split() error
	Phase() (string, error)
}

type Status struct {
	Enabled    bool    `json:"enabled"`
	Profile    string  `json:"profile"` // name of the watched profile
	File       string  `json:"file"`    // its file in the Save folder
	Beaten     int     `json:"beaten"`
	Splits     int     `json:"splits"`
	TimerName  string  `json:"timerName"`
	LiveSplit  string  `json:"liveSplit"` // address or URL of the timer
	Connected  bool    `json:"connected"`
	TimerPhase string  `json:"timerPhase"`
	LinkError  string  `json:"linkError"`
	Events     []Event `json:"events"`
}

type Watcher struct {
	timer    Timer
	logDir   string
	interval time.Duration

	mu      sync.Mutex
	enabled bool
	profile string
	path    string
	stop    chan struct{}
	done    chan struct{}
	last    []byte
	lastP   *czp.Profile
	badRead int
	splits  int
	events  []Event
	nextID  int64
}

const keepEvents = 300

func NewWatcher(timer Timer, logDir string) *Watcher {
	return &Watcher{timer: timer, logDir: logDir, interval: 50 * time.Millisecond}
}

// SetTimer switches between LiveSplit and LiveSplit One.
func (w *Watcher) SetTimer(t Timer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer = t
}

func (w *Watcher) currentTimer() Timer {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.timer
}

func (w *Watcher) addLocked(kind, f string, a ...any) {
	w.nextID++
	e := Event{ID: w.nextID, Time: time.Now(), Kind: kind, Text: fmt.Sprintf(f, a...)}
	w.events = append(w.events, e)
	if len(w.events) > keepEvents {
		w.events = w.events[len(w.events)-keepEvents:]
	}
	if w.logDir != "" {
		if os.MkdirAll(w.logDir, 0o755) == nil {
			name := filepath.Join(w.logDir, "autosplit-"+e.Time.Format("2006-01-02")+".log")
			if f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintf(f, "%s  %-5s %s\n", e.Time.Format("15:04:05.000"), e.Kind, e.Text)
				f.Close()
			}
		}
	}
}

// Start watches path (the CZProfile file of profile). The current content is
// the baseline: nothing is split for what is already beaten.
func (w *Watcher) Start(profile, path string) {
	w.Stop()
	w.mu.Lock()
	w.enabled, w.profile, w.path = true, profile, path
	w.last, w.lastP, w.badRead, w.splits = nil, nil, 0, 0
	if data, err := os.ReadFile(path); err == nil {
		if p, err := czp.Parse(data); err == nil && p.ChecksumOK {
			w.last, w.lastP = data, p
			w.addLocked("info", "Watching %s: %d maps beaten. Start your timer in LiveSplit; each map you win splits.", profile, p.BeatenTotal())
		}
	}
	if w.lastP == nil {
		w.addLocked("warn", "Watching %s: the profile does not exist yet or cannot be read; waiting for the game to write it.", profile)
	}
	w.stop, w.done = make(chan struct{}), make(chan struct{})
	stop, done := w.stop, w.done
	w.mu.Unlock()
	go w.loop(stop, done)
}

func (w *Watcher) Stop() {
	w.mu.Lock()
	stop, done := w.stop, w.done
	wasOn := w.enabled
	w.enabled, w.stop, w.done = false, nil, nil
	if wasOn {
		w.addLocked("info", "Stopped watching %s.", w.profile)
	}
	w.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

func (w *Watcher) loop(stop, done chan struct{}) {
	defer close(done)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			w.check()
		}
	}
}

func (w *Watcher) check() {
	w.mu.Lock()
	path := w.path
	w.mu.Unlock()
	data, err := os.ReadFile(path)
	if err != nil {
		return // missing or being written: try again next tick
	}
	w.mu.Lock()
	if bytes.Equal(data, w.last) {
		w.mu.Unlock()
		return
	}
	p, err := czp.Parse(data)
	if err != nil || !p.ChecksumOK {
		// Probably caught mid-write; give it a few ticks before complaining.
		w.badRead++
		if w.badRead == 40 {
			w.addLocked("error", "%s cannot be read as a career profile.", filepath.Base(path))
		}
		w.mu.Unlock()
		return
	}
	w.badRead = 0
	prev := w.lastP
	w.last, w.lastP = data, p
	won := newlyBeaten(prev, p)
	if prev != nil && p.BeatenTotal() < prev.BeatenTotal() {
		w.addLocked("reset", "Profile restored: %d maps beaten (was %d). Reset your timer in LiveSplit if needed.", p.BeatenTotal(), prev.BeatenTotal())
	} else if len(won) == 0 {
		w.addLocked("save", "The game saved the profile: %s", describe(p, prev))
	}
	timer := w.timer
	w.mu.Unlock()

	for _, m := range won {
		phase, _ := timer.Phase()
		err := timer.Split()
		w.mu.Lock()
		switch {
		case err != nil:
			w.addLocked("error", "%s won, but the split was not sent: %v", m, err)
		default:
			w.splits++
			msg := fmt.Sprintf("Split: %s won (%s)", m, describe(p, prev))
			if phase == "NotRunning" {
				msg += ". The timer was not running, so " + timer.Name() + " ignored it"
			}
			w.addLocked("split", "%s", msg)
		}
		w.mu.Unlock()
	}
}

// newlyBeaten lists the maps beaten in p but not in prev, per campaign.
func newlyBeaten(prev, p *czp.Profile) []string {
	if prev == nil {
		return nil
	}
	var out []string
	for d, c := range p.Campaigns {
		if c == nil {
			continue
		}
		before := map[string]bool{}
		if d < len(prev.Campaigns) && prev.Campaigns[d] != nil {
			for _, m := range prev.Campaigns[d].Maps {
				before[m.Name] = m.Beaten
			}
		}
		for _, m := range c.Maps {
			if m.Beaten && !before[m.Name] {
				name := game.DisplayName(m.Name)
				if d < len(game.DifficultyNames) {
					name += " " + game.DifficultyNames[d]
				}
				out = append(out, name)
			}
		}
	}
	return out
}

func describe(p, prev *czp.Profile) string {
	var parts []string
	for d, c := range p.Campaigns {
		if c == nil || d >= len(game.DifficultyNames) {
			continue
		}
		team := "no team"
		if len(c.Team) > 0 {
			team = "team " + strings.Join(c.Team, ", ")
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d maps, %d points, %s, map %s",
			game.DifficultyNames[d], c.BeatenCount(), len(c.Maps), c.Points, team, game.DisplayName(c.CurrentMap)))
	}
	// The unknown field is logged to learn what it means.
	if prev == nil || prev.Unknown != p.Unknown {
		parts = append(parts, fmt.Sprintf("unknown value %d", p.Unknown))
	}
	return strings.Join(parts, "; ")
}

// Status returns the state and the events newer than sinceID.
func (w *Watcher) Status(sinceID int64) Status {
	timer := w.currentTimer()
	phase, linkErr := timer.Phase()
	w.mu.Lock()
	defer w.mu.Unlock()
	st := Status{Enabled: w.enabled, Profile: w.profile, File: filepath.Base(w.path), Splits: w.splits, TimerName: timer.Name(),
		LiveSplit: timer.Describe(), Connected: linkErr == nil, TimerPhase: phase}
	if linkErr != nil {
		st.LinkError = linkErr.Error()
	}
	if w.lastP != nil {
		st.Beaten = w.lastP.BeatenTotal()
	}
	for _, e := range w.events {
		if e.ID > sinceID {
			st.Events = append(st.Events, e)
		}
	}
	return st
}
