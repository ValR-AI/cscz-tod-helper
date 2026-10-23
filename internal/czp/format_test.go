package czp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Real profiles written by the game must parse and re-encode byte for byte.
// Point CZP_FIXTURES at a folder of CZProfile-*.czp files to run it.
func TestRealProfilesRoundTrip(t *testing.T) {
	dir := os.Getenv("CZP_FIXTURES")
	if dir == "" {
		t.Skip("CZP_FIXTURES not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.czp"))
	if len(files) == 0 {
		t.Fatal("no .czp in CZP_FIXTURES")
	}
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		p, err := Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !p.ChecksumOK {
			t.Errorf("%s: checksum not recognised", f)
		}
		out, err := p.Bytes()
		if err != nil || !bytes.Equal(out, raw) {
			t.Errorf("%s: round trip differs (%v)", f, err)
		}
	}
}

func TestChecksumUsesUnsignedBytes(t *testing.T) {
	if Checksum([]byte{0xF4, 1}) != 0xF4*1+1*2 {
		t.Fatal("bytes must be unsigned")
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("hello")); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := Parse([]byte("CZP\x00\x04\x00\x00\x00abc")); err == nil {
		t.Fatal("expected a truncation error")
	}
}
