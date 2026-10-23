package autosplit

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"todhelper/internal/savestate"
)

// fakeLiveSplit records the commands it receives and answers phase queries.
type fakeLiveSplit struct {
	ln   net.Listener
	mu   sync.Mutex
	cmds []string
}

func startFake(t *testing.T) *fakeLiveSplit {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLiveSplit{ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.TrimSpace(line)
					f.mu.Lock()
					f.cmds = append(f.cmds, cmd)
					f.mu.Unlock()
					if cmd == "getcurrenttimerphase" {
						c.Write([]byte("Running\r\n"))
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeLiveSplit) splits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cmds {
		if c == "split" {
			n++
		}
	}
	return n
}

func write(t *testing.T, path string, s savestate.Savestate) {
	data, err := savestate.Bytes(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := savestate.WriteFile(path, data, false); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatcherSplitsOnWins(t *testing.T) {
	fake := startFake(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "CZProfile-Run.czp")
	tour1 := []string{"de_dust_cz", "cs_office_cz", "de_prodigy_cz"}
	base := savestate.Savestate{Name: "Run", Beaten: tour1, Current: "de_tides_cz"}
	write(t, path, base)

	w := NewWatcher(NewLiveSplit(fake.ln.Addr().String()), filepath.Join(dir, "logs"))
	w.interval = 5 * time.Millisecond
	w.Start("Run", path)
	defer w.Stop()

	// The game saves after hiring a team: no split.
	withTeam := base
	withTeam.Team = []string{"floyd", "morris"}
	write(t, path, withTeam)
	waitFor(t, "save event", func() bool { return hasEvent(w, "save") })
	if fake.splits() != 0 {
		t.Fatal("a save without a new win must not split")
	}

	// A half-written file is ignored.
	os.WriteFile(path, []byte("CZP\x00garbage"), 0o644)
	time.Sleep(30 * time.Millisecond)

	// Tides won: one split.
	won := withTeam
	won.Beaten = append(append([]string(nil), tour1...), "de_tides_cz")
	write(t, path, won)
	waitFor(t, "one split", func() bool { return fake.splits() == 1 })
	if st := w.Status(0); st.Splits != 1 || st.Beaten != 4 || !st.Connected || st.TimerPhase != "Running" {
		t.Fatalf("status: %+v", st)
	}

	// Italy and Inferno won: two more splits.
	won.Beaten = append(won.Beaten, "cs_italy_cz")
	write(t, path, won)
	waitFor(t, "two splits", func() bool { return fake.splits() == 2 })
	won.Beaten = append(won.Beaten, "de_inferno_cz")
	write(t, path, won)
	waitFor(t, "three splits", func() bool { return fake.splits() == 3 })

	// The red button restores the savestate: no split, a reset event.
	write(t, path, base)
	waitFor(t, "reset event", func() bool { return hasEvent(w, "reset") })
	if fake.splits() != 3 {
		t.Fatal("restoring must not split")
	}
	logs, _ := filepath.Glob(filepath.Join(dir, "logs", "*.log"))
	if len(logs) != 1 {
		t.Fatal("expected a log file")
	}
}

func TestWatcherWithoutLiveSplit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CZProfile-Run.czp")
	write(t, path, savestate.Savestate{Name: "Run"})
	w := NewWatcher(NewLiveSplit("127.0.0.1:1"), "")
	w.interval = 5 * time.Millisecond
	w.Start("Run", path)
	defer w.Stop()
	write(t, path, savestate.Savestate{Name: "Run", Beaten: []string{"de_dust_cz"}})
	waitFor(t, "error event", func() bool { return hasEvent(w, "error") })
	if st := w.Status(0); st.Connected || st.LinkError == "" {
		t.Fatalf("status should show LiveSplit unreachable: %+v", st)
	}
}

func hasEvent(w *Watcher, kind string) bool {
	for _, e := range w.Status(0).Events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}
