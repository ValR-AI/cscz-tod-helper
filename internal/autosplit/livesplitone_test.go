package autosplit

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"todhelper/internal/savestate"
)

// fakeLSO plays LiveSplit One's side: it connects, answers getCurrentState
// like livesplit-core does and records the commands it receives.
type fakeLSO struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	cmds []string
}

func dialLSO(t *testing.T, addr string) *fakeLSO {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	io.WriteString(conn, "GET /livesplit-one HTTP/1.1\r\nHost: "+addr+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: "+key+"\r\nSec-WebSocket-Version: 13\r\nOrigin: https://one.livesplit.org\r\n\r\n")
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	if resp.StatusCode != 101 || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Fatalf("bad handshake: %d %v", resp.StatusCode, resp.Header)
	}
	f := &fakeLSO{conn: conn, r: r}
	go f.loop()
	return f
}

func (f *fakeLSO) send(opcode byte, payload string) {
	mask := [4]byte{1, 2, 3, 4}
	b := []byte{0x80 | opcode}
	n := len(payload)
	if n < 126 {
		b = append(b, 0x80|byte(n))
	} else {
		b = append(b, 0x80|126)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	}
	b = append(b, mask[:]...)
	for i := 0; i < n; i++ {
		b = append(b, payload[i]^mask[i%4])
	}
	f.conn.Write(b)
}

func (f *fakeLSO) loop() {
	for {
		var h [2]byte
		if _, err := io.ReadFull(f.r, h[:]); err != nil {
			return
		}
		if h[1]&0x80 != 0 {
			panic("server frames must not be masked")
		}
		n := int(h[1] & 0x7F)
		if n == 126 {
			var b [2]byte
			io.ReadFull(f.r, b[:])
			n = int(binary.BigEndian.Uint16(b[:]))
		}
		payload := make([]byte, n)
		io.ReadFull(f.r, payload)
		switch h[0] & 0x0F {
		case 0x1:
			cmd := string(payload)
			f.mu.Lock()
			f.cmds = append(f.cmds, cmd)
			f.mu.Unlock()
			if strings.Contains(cmd, "getCurrentState") {
				f.send(0x1, `{"success":{"state":"Running","index":0}}`)
			} else {
				f.send(0x1, `{"event":"Splitted"}`)
				f.send(0x1, `{"success":null}`)
			}
		case 0xA:
			f.mu.Lock()
			f.cmds = append(f.cmds, "pong:"+string(payload))
			f.mu.Unlock()
		}
	}
}

func (f *fakeLSO) count(cmd string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cmds {
		if c == cmd {
			n++
		}
	}
	return n
}

func TestLiveSplitOne(t *testing.T) {
	lso := NewLiveSplitOne("ws://127.0.0.1:47634/livesplit-one")
	if err := lso.Split(); err == nil || !strings.Contains(err.Error(), "Connect to Server") {
		t.Fatalf("splitting with nobody connected must explain how to connect: %v", err)
	}
	srv := httptest.NewServer(lso)
	defer srv.Close()

	// A plain HTTP visit gets an explanation, not a crash.
	resp, err := http.Get(srv.URL)
	if err != nil || resp.StatusCode != 400 {
		t.Fatalf("plain GET: %v %v", resp, err)
	}

	client := dialLSO(t, strings.TrimPrefix(srv.URL, "http://"))
	defer client.conn.Close()
	waitFor(t, "state query", func() bool { return client.count(`{"command":"getCurrentState"}`) > 0 })
	waitFor(t, "phase", func() bool { p, err := lso.Phase(); return err == nil && p == "Running" })

	client.send(0x9, "hi") // ping
	waitFor(t, "pong", func() bool { return client.count("pong:hi") == 1 })

	// The watcher splits LiveSplit One when the game saves a newly won map.
	dir := t.TempDir()
	path := filepath.Join(dir, "CZProfile-Run.czp")
	write(t, path, savestate.Savestate{Name: "Run"})
	w := NewWatcher(lso, "")
	w.interval = 5 * time.Millisecond
	w.Start("Run", path)
	defer w.Stop()
	write(t, path, savestate.Savestate{Name: "Run", Beaten: []string{"de_dust_cz"}})
	waitFor(t, "split", func() bool { return client.count(`{"command":"split"}`) == 1 })
	if st := w.Status(0); st.Splits != 1 || st.TimerName != "LiveSplit One" || !st.Connected {
		t.Fatalf("status: %+v", st)
	}

	// Closing the connection disconnects it.
	client.send(0x8, "")
	waitFor(t, "disconnect", func() bool { return lso.Clients() == 0 })
	if _, err := lso.Phase(); err == nil {
		t.Fatal("phase should report the disconnection")
	}
}
