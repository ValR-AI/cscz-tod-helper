package autosplit

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LiveSplitOne drives LiveSplit One (web or desktop, Windows/Linux/macOS)
// through its "Connect to Server" feature: LiveSplit One connects to this
// WebSocket endpoint and runs the JSON commands it receives, such as
// {"command":"split"} (livesplit-core, src/networking/server_protocol.rs).
//
// The WebSocket protocol (RFC 6455) is implemented here with the standard
// library only, to keep the helper free of dependencies.
type LiveSplitOne struct {
	URL string // shown to the user, e.g. ws://127.0.0.1:47634/livesplit-one

	mu      sync.Mutex
	clients map[*wsConn]bool
	phase   string
	lastErr string
}

func NewLiveSplitOne(url string) *LiveSplitOne {
	return &LiveSplitOne{URL: url, clients: map[*wsConn]bool{}}
}

func (l *LiveSplitOne) Name() string     { return "LiveSplit One" }
func (l *LiveSplitOne) Describe() string { return l.URL }

func (l *LiveSplitOne) Clients() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

func (l *LiveSplitOne) notConnected() error {
	return fmt.Errorf("LiveSplit One is not connected (in LiveSplit One: Settings > Connect to Server > %s)", l.URL)
}

// Split sends {"command":"split"} to every connected LiveSplit One.
func (l *LiveSplitOne) Split() error {
	l.mu.Lock()
	clients := make([]*wsConn, 0, len(l.clients))
	for c := range l.clients {
		clients = append(clients, c)
	}
	l.mu.Unlock()
	if len(clients) == 0 {
		return l.notConnected()
	}
	sent := 0
	for _, c := range clients {
		if err := c.writeText(`{"command":"split"}`); err != nil {
			l.drop(c)
			continue
		}
		sent++
	}
	if sent == 0 {
		return l.notConnected()
	}
	return nil
}

// Phase is the timer phase reported by LiveSplit One (asked every second).
func (l *LiveSplitOne) Phase() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.clients) == 0 {
		return "", l.notConnected()
	}
	if l.phase == "" {
		return "connected", nil
	}
	return l.phase, nil
}

func (l *LiveSplitOne) drop(c *wsConn) {
	l.mu.Lock()
	delete(l.clients, c)
	if len(l.clients) == 0 {
		l.phase = ""
	}
	l.mu.Unlock()
	c.close()
}

// handleMessage reads LiveSplit One's answers; only the timer state matters.
func (l *LiveSplitOne) handleMessage(msg []byte) {
	var m struct {
		Success *struct {
			State string `json:"state"`
		} `json:"success"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(msg, &m) != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if m.Success != nil && m.Success.State != "" {
		l.phase = m.Success.State
	}
	if m.Error != nil {
		l.lastErr = m.Error.Code
	}
}

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ServeHTTP upgrades the request to a WebSocket and keeps the connection.
func (l *LiveSplitOne) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" || r.Header.Get("Sec-WebSocket-Version") != "13" {
		http.Error(w, "this address is for LiveSplit One: Settings > Connect to Server", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket not supported", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	conn.SetDeadline(time.Time{})
	if _, err := fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		conn.Close()
		return
	}
	c := &wsConn{conn: conn, r: rw.Reader, done: make(chan struct{})}
	l.mu.Lock()
	l.clients[c] = true
	l.mu.Unlock()

	go func() { // ask for the timer state every second (also keeps the link alive)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			if c.writeText(`{"command":"getCurrentState"}`) != nil {
				l.drop(c)
				return
			}
			select {
			case <-c.done:
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		defer l.drop(c)
		for {
			msg, err := c.readMessage()
			if err != nil {
				return
			}
			l.handleMessage(msg)
		}
	}()
}

// ---------------------------------------------------------------------------
// Minimal RFC 6455 server connection
// ---------------------------------------------------------------------------

type wsConn struct {
	conn net.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
	once sync.Once
	done chan struct{}
}

func (c *wsConn) close() {
	c.once.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	hdr := []byte{0x80 | opcode} // FIN + opcode; server frames are not masked
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.conn.Write(append(hdr, payload...)); err != nil {
		return err
	}
	return nil
}

func (c *wsConn) writeText(s string) error { return c.writeFrame(0x1, []byte(s)) }

const maxMessage = 1 << 20

// readMessage returns the next text message, answering pings and closes.
func (c *wsConn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.r, h[:]); err != nil {
			return nil, err
		}
		fin, opcode := h[0]&0x80 != 0, h[0]&0x0F
		masked, n := h[1]&0x80 != 0, uint64(h[1]&0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if !masked {
			return nil, errors.New("websocket: client frames must be masked")
		}
		if n > maxMessage || uint64(len(msg))+n > maxMessage {
			return nil, errors.New("websocket: message too large")
		}
		var mask [4]byte
		if _, err := io.ReadFull(c.r, mask[:]); err != nil {
			return nil, err
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return nil, err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		switch opcode {
		case 0x8: // close
			c.writeFrame(0x8, nil)
			return nil, io.EOF
		case 0x9: // ping
			if err := c.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA: // pong
			continue
		case 0x0, 0x1, 0x2: // continuation, text, binary
			msg = append(msg, payload...)
			if fin {
				if opcode == 0x2 {
					msg = nil // binary messages are not part of the protocol
					continue
				}
				return msg, nil
			}
		default:
			return nil, fmt.Errorf("websocket: unknown opcode %d", opcode)
		}
	}
}
