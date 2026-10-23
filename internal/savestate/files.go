package savestate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"todhelper/internal/czp"
)

// The helper only ever creates, rewrites or deletes CZProfile-*.czp files in
// the Save folder; nothing else of the game is touched. A profile is first
// written under a temporary name (.cscz-tod-helper-CZProfile-<name>.czp.tmp),
// then renamed, so the game never reads a half-written profile.
//
// On Windows, Go maps the owner-write permission bit to the file's
// read-only attribute, so the same code works on both systems.

func Path(saveDir, name string) string { return filepath.Join(saveDir, FileName(name)) }

func IsReadOnly(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o200 == 0
}

func setWritable(path string, writable bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if writable {
		mode |= 0o200
	} else {
		mode &^= 0o222
	}
	return os.Chmod(path, mode)
}

// CheckSaveDir makes sure the Save folder exists.
func CheckSaveDir(saveDir string) error {
	if strings.TrimSpace(saveDir) == "" {
		return errors.New("the Save folder is not set (Settings)")
	}
	info, err := os.Stat(saveDir)
	if err != nil {
		return fmt.Errorf("the Save folder %s does not exist", saveDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a folder", saveDir)
	}
	return nil
}

// TempPath is where a profile is written before it takes its real name. It
// does not start with CZProfile-, so the game never lists it.
func TempPath(path string) string {
	return filepath.Join(filepath.Dir(path), ".cscz-tod-helper-"+filepath.Base(path)+".tmp")
}

// WriteFile writes data as a profile, keeping or removing the read-only
// flag. The whole file is written under a temporary name, then renamed over
// the profile in one step.
func WriteFile(path string, data []byte, readOnly bool) error {
	if _, err := os.Stat(path); err == nil && IsReadOnly(path) {
		if err := setWritable(path, true); err != nil {
			return err
		}
	}
	tmp := TempPath(path)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	serr := f.Sync()
	if err := errors.Join(werr, serr, f.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return setWritable(path, !readOnly)
}

// Install writes a savestate's profile into the Save folder.
func Install(saveDir string, s Savestate) error {
	if err := CheckSaveDir(saveDir); err != nil {
		return err
	}
	data, err := Bytes(s)
	if err != nil {
		return err
	}
	return WriteFile(Path(saveDir, Evaluate(s).Savestate.Name), data, s.ReadOnly)
}

// Remove deletes a profile from the Save folder (no error if it is missing).
func Remove(saveDir, name string) error {
	if CheckName(name) != nil {
		return nil
	}
	path := Path(saveDir, name)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if IsReadOnly(path) {
		if err := setWritable(path, true); err != nil {
			return err
		}
	}
	return os.Remove(path)
}

// Status compares the profile in the Save folder with the savestate.
type Status struct {
	State    string `json:"state"` // ready, changed, missing, unknown
	ReadOnly bool   `json:"readOnly"`
	Beaten   int    `json:"beaten"` // beaten maps in the file, when readable
}

func FileStatus(saveDir string, s Savestate) Status {
	if CheckSaveDir(saveDir) != nil {
		return Status{State: "unknown"}
	}
	path := Path(saveDir, Evaluate(s).Savestate.Name)
	data, err := os.ReadFile(path)
	if err != nil {
		return Status{State: "missing"}
	}
	st := Status{State: "changed", ReadOnly: IsReadOnly(path)}
	if p, err := czp.Parse(data); err == nil {
		st.Beaten = p.BeatenTotal()
	}
	if want, err := Bytes(s); err == nil && bytes.Equal(want, data) && st.ReadOnly == s.ReadOnly {
		st.State = "ready"
	}
	return st
}

// ProfileFile is a profile found in the Save folder.
type ProfileFile struct {
	File       string `json:"file"`
	Name       string `json:"name"` // the name written in the profile (the one the game shows)
	Beaten     int    `json:"beaten"`
	Valid      bool   `json:"valid"`
	ReadOnly   bool   `json:"readOnly"`
	Difficulty string `json:"difficulty"` // current difficulty, "" when no career is started
}

// IsProfileFile tells whether a file name looks like a career profile.
func IsProfileFile(name string) bool {
	return name == filepath.Base(name) && strings.HasPrefix(name, "CZProfile-") && strings.HasSuffix(name, ".czp") && len(name) > len("CZProfile-.czp")
}

// ReadProfileFile reads one profile of the Save folder.
func ReadProfileFile(saveDir, file string) ProfileFile {
	pf := ProfileFile{File: file, Name: strings.TrimSuffix(strings.TrimPrefix(file, "CZProfile-"), ".czp")}
	path := filepath.Join(saveDir, file)
	pf.ReadOnly = IsReadOnly(path)
	if data, err := os.ReadFile(path); err == nil {
		if p, err := czp.Parse(data); err == nil {
			pf.Valid = p.ChecksumOK
			pf.Beaten = p.BeatenTotal()
			if p.Name != "" {
				pf.Name = p.Name
			}
			if p.Difficulty < 4 && p.Difficulty < len(p.Campaigns) && p.Campaigns[p.Difficulty] != nil {
				pf.Difficulty = []string{"easy", "normal", "hard", "expert"}[p.Difficulty]
			}
		}
	}
	return pf
}

// ListProfiles lists the CZProfile-*.czp files of the Save folder.
func ListProfiles(saveDir string) ([]ProfileFile, error) {
	if err := CheckSaveDir(saveDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(saveDir)
	if err != nil {
		return nil, err
	}
	var out []ProfileFile
	for _, e := range entries {
		if e.IsDir() || !IsProfileFile(e.Name()) {
			continue
		}
		out = append(out, ReadProfileFile(saveDir, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}
