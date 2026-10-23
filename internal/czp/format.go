// Package czp reads and writes Counter-Strike: Condition Zero career
// profiles (CZProfile-<name>.czp).
//
// Integers are little-endian, strings are zero-terminated (Latin-1).
//
//	char[4]  "CZP\0"
//	int32    campaign count (4: Easy, Normal, Hard, Expert)
//	string   profile name (also the in-game player name)
//	int32    unknown value written by the game (0 in generated profiles)
//	byte     current difficulty (0 Easy, 1 Normal, 2 Hard, 3 Expert)
//	byte     medal per campaign [campaign count]
//	per campaign:
//	  byte   started (0/1); when 1:
//	    byte    reputation points
//	    byte    team size, then the teammate names
//	    byte    map count, then per map: name, byte beaten (0/1)
//	    string  current map
//	    byte    reputation points before the last win
//	int32    reserved size (256), then that many int32 set to 0
//	uint32   checksum = sum of byte[i] * (i + 1) over everything before it
//	         (unsigned bytes, 32-bit wrap-around)
package czp

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const ReservedInts = 256

type MapState struct {
	Name   string
	Beaten bool
}

type Campaign struct {
	Points     int
	Team       []string
	Maps       []MapState
	CurrentMap string
	PrevPoints int
}

func (c *Campaign) BeatenCount() int {
	n := 0
	for _, m := range c.Maps {
		if m.Beaten {
			n++
		}
	}
	return n
}

type Profile struct {
	Name       string
	Unknown    int32
	Difficulty int
	Medals     []byte
	Campaigns  []*Campaign // nil = campaign not started
	Reserved   []byte
	ChecksumOK bool
}

// BeatenTotal counts the beaten maps over every started campaign.
func (p *Profile) BeatenTotal() int {
	n := 0
	for _, c := range p.Campaigns {
		if c != nil {
			n += c.BeatenCount()
		}
	}
	return n
}

func Checksum(b []byte) uint32 {
	var sum uint32
	for i, v := range b {
		sum += uint32(v) * uint32(i+1)
	}
	return sum
}

type parseError string

type reader struct {
	b []byte
	p int
}

func (r *reader) need(n int) {
	if r.p+n > len(r.b) {
		panic(parseError(fmt.Sprintf("file is truncated (offset 0x%x)", r.p)))
	}
}

func (r *reader) u8() int {
	r.need(1)
	r.p++
	return int(r.b[r.p-1])
}

func (r *reader) i32() int32 {
	r.need(4)
	r.p += 4
	return int32(binary.LittleEndian.Uint32(r.b[r.p-4:]))
}

func (r *reader) str() string {
	end := bytes.IndexByte(r.b[r.p:], 0)
	if end < 0 {
		panic(parseError(fmt.Sprintf("unterminated string (offset 0x%x)", r.p)))
	}
	rs := make([]rune, end)
	for i, c := range r.b[r.p : r.p+end] {
		rs[i] = rune(c)
	}
	r.p += end + 1
	return string(rs)
}

// Parse decodes a profile. ChecksumOK tells whether the stored checksum matches.
func Parse(b []byte) (p *Profile, err error) {
	defer func() {
		if e := recover(); e != nil {
			if pe, ok := e.(parseError); ok {
				p, err = nil, fmt.Errorf("%s", string(pe))
				return
			}
			panic(e)
		}
	}()
	if len(b) < 4 || string(b[:4]) != "CZP\x00" {
		return nil, fmt.Errorf("not a Condition Zero career profile (no CZP header)")
	}
	r := &reader{b: b, p: 4}
	count := int(r.i32())
	if count < 1 || count > 16 {
		return nil, fmt.Errorf("unexpected campaign count: %d", count)
	}
	p = &Profile{}
	p.Name = r.str()
	p.Unknown = r.i32()
	p.Difficulty = r.u8()
	for i := 0; i < count; i++ {
		p.Medals = append(p.Medals, byte(r.u8()))
	}
	for i := 0; i < count; i++ {
		if r.u8() == 0 {
			p.Campaigns = append(p.Campaigns, nil)
			continue
		}
		c := &Campaign{Points: r.u8()}
		for n := r.u8(); n > 0; n-- {
			c.Team = append(c.Team, r.str())
		}
		for n := r.u8(); n > 0; n-- {
			name := r.str()
			c.Maps = append(c.Maps, MapState{Name: name, Beaten: r.u8() != 0})
		}
		c.CurrentMap = r.str()
		c.PrevPoints = r.u8()
		p.Campaigns = append(p.Campaigns, c)
	}
	size := int(r.i32())
	if size < 0 || size > 1<<20 {
		return nil, fmt.Errorf("invalid reserved size: %d", size)
	}
	r.need(size * 4)
	p.Reserved = append([]byte(nil), r.b[r.p:r.p+size*4]...)
	r.p += size * 4
	if r.p+4 <= len(b) {
		p.ChecksumOK = binary.LittleEndian.Uint32(b[r.p:]) == Checksum(b[:r.p])
	}
	return p, nil
}

// Bytes encodes the profile and appends a valid checksum.
func (p *Profile) Bytes() ([]byte, error) {
	var w bytes.Buffer
	var err error
	u8 := func(v int) {
		if v < 0 || v > 255 {
			err = fmt.Errorf("value out of range (0-255): %d", v)
		}
		w.WriteByte(byte(v))
	}
	str := func(s string) {
		for _, r := range s {
			if r == 0 || r > 255 {
				err = fmt.Errorf("unsupported character in %q", s)
			}
			w.WriteByte(byte(r))
		}
		w.WriteByte(0)
	}
	i32 := func(v int32) { binary.Write(&w, binary.LittleEndian, v) }

	if len(p.Medals) != len(p.Campaigns) {
		return nil, fmt.Errorf("medal count does not match campaign count")
	}
	w.WriteString("CZP\x00")
	i32(int32(len(p.Campaigns)))
	str(p.Name)
	i32(p.Unknown)
	u8(p.Difficulty)
	for _, m := range p.Medals {
		u8(int(m))
	}
	for _, c := range p.Campaigns {
		if c == nil {
			u8(0)
			continue
		}
		u8(1)
		u8(c.Points)
		u8(len(c.Team))
		for _, t := range c.Team {
			str(t)
		}
		u8(len(c.Maps))
		for _, m := range c.Maps {
			str(m.Name)
			if m.Beaten {
				u8(1)
			} else {
				u8(0)
			}
		}
		str(c.CurrentMap)
		u8(c.PrevPoints)
	}
	i32(int32(len(p.Reserved) / 4))
	w.Write(p.Reserved)
	if err != nil {
		return nil, err
	}
	binary.Write(&w, binary.LittleEndian, Checksum(w.Bytes()))
	return w.Bytes(), nil
}
