package autosplit

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// LiveSplit talks to LiveSplit's TCP server (Control > Start TCP Server,
// port 16834 by default). Commands are lines ending with \r\n; only queries
// get an answer.
type LiveSplit struct {
	mu   sync.Mutex
	addr string
	conn net.Conn
	r    *bufio.Reader

	phase     string
	phaseErr  error
	phaseTime time.Time
}

func NewLiveSplit(addr string) *LiveSplit { return &LiveSplit{addr: addr} }

func (l *LiveSplit) SetAddr(addr string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if addr != l.addr {
		l.closeLocked()
		l.addr = addr
		l.phaseTime = time.Time{}
	}
}

func (l *LiveSplit) Addr() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.addr
}

func (l *LiveSplit) closeLocked() {
	if l.conn != nil {
		l.conn.Close()
	}
	l.conn, l.r = nil, nil
}

func (l *LiveSplit) connectLocked() error {
	if l.conn != nil {
		return nil
	}
	c, err := net.DialTimeout("tcp", l.addr, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("LiveSplit is not reachable at %s (start its TCP server)", l.addr)
	}
	l.conn, l.r = c, bufio.NewReader(c)
	return nil
}

// do sends a command, reconnecting once if the connection went stale, and
// reads one answer line when wantAnswer is set.
func (l *LiveSplit) do(cmd string, wantAnswer bool) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := l.connectLocked(); err != nil {
			return "", err
		}
		l.conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := l.conn.Write([]byte(cmd + "\r\n")); err != nil {
			lastErr = err
			l.closeLocked()
			continue
		}
		if !wantAnswer {
			return "", nil
		}
		line, err := l.r.ReadString('\n')
		if err != nil {
			lastErr = err
			l.closeLocked()
			continue
		}
		return strings.TrimSpace(line), nil
	}
	return "", fmt.Errorf("LiveSplit connection lost: %v", lastErr)
}

func (l *LiveSplit) Name() string     { return "LiveSplit" }
func (l *LiveSplit) Describe() string { return l.Addr() }

func (l *LiveSplit) Split() error { _, err := l.do("split", false); return err }

// Phase returns the timer phase (NotRunning, Running, Ended, Paused), cached
// for half a second so the interface can poll it freely.
func (l *LiveSplit) Phase() (string, error) {
	l.mu.Lock()
	if time.Since(l.phaseTime) < 500*time.Millisecond {
		p, err := l.phase, l.phaseErr
		l.mu.Unlock()
		return p, err
	}
	l.mu.Unlock()
	p, err := l.do("getcurrenttimerphase", true)
	l.mu.Lock()
	l.phase, l.phaseErr, l.phaseTime = p, err, time.Now()
	l.mu.Unlock()
	return p, err
}

func (l *LiveSplit) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeLocked()
}
