// Package bundle stores the user's bundles: named lists of savestates kept
// as JSON files in the helper's data folder. A bundle is the reference image
// that the red reset button writes back into the game's Save folder.
package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"todhelper/internal/savestate"
)

type Bundle struct {
	Name       string                `json:"name"`
	Savestates []savestate.Savestate `json:"savestates"`
}

type Store struct{ Dir string }

const maxNameLength = 40

func CheckName(name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return errors.New("the bundle name is empty or starts/ends with a space")
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("the bundle name is too long (%d characters max)", maxNameLength)
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(" _-.()", r)
		if !ok {
			return fmt.Errorf("%q is not allowed in a bundle name (letters, digits, space and _-.())", r)
		}
	}
	if strings.HasPrefix(name, ".") {
		return errors.New("a bundle name cannot start with a dot")
	}
	return nil
}

func (s Store) path(name string) string { return filepath.Join(s.Dir, name+".json") }

func (s Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && CheckName(n) == nil {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names, nil
}

func (s Store) Exists(name string) bool {
	_, err := os.Stat(s.path(name))
	return err == nil
}

func (s Store) Load(name string) (*Bundle, error) {
	if err := CheckName(name); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return nil, fmt.Errorf("bundle %q not found", name)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("bundle %q is damaged: %w", name, err)
	}
	b.Name = name
	return &b, nil
}

func (s Store) Save(b *Bundle) error {
	if err := CheckName(b.Name); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if b.Savestates == nil {
		b.Savestates = []savestate.Savestate{}
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(b.Name) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(b.Name))
}

func (s Store) Create(name string) (*Bundle, error) {
	if err := CheckName(name); err != nil {
		return nil, err
	}
	if s.Exists(name) {
		return nil, fmt.Errorf("a bundle named %q already exists", name)
	}
	b := &Bundle{Name: name}
	return b, s.Save(b)
}

func (s Store) Rename(oldName, newName string) error {
	if err := CheckName(newName); err != nil {
		return err
	}
	b, err := s.Load(oldName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(oldName, newName) && s.Exists(newName) {
		return fmt.Errorf("a bundle named %q already exists", newName)
	}
	if oldName == newName {
		return nil
	}
	// Move the old file aside first: on Windows a case-only rename points to
	// the same file.
	aside := s.path(oldName) + ".old"
	if err := os.Rename(s.path(oldName), aside); err != nil {
		return err
	}
	b.Name = newName
	if err := s.Save(b); err != nil {
		os.Rename(aside, s.path(oldName))
		return err
	}
	return os.Remove(aside)
}

func (s Store) Delete(name string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	return os.Remove(s.path(name))
}

// Find returns the index of a savestate by profile name (case-insensitive,
// like file names on Windows), or -1.
// Find returns the savestate using the same profile file as name (same
// name, or a name the game turns into the same file), or -1.
func (b *Bundle) Find(name string) int {
	for i, s := range b.Savestates {
		if savestate.SameFile(s.Name, name) {
			return i
		}
	}
	return -1
}

// Put adds a savestate, or replaces the one called original when editing.
func (b *Bundle) Put(s savestate.Savestate, original string) error {
	idx := -1
	if original != "" {
		idx = b.Find(original)
	}
	if other := b.Find(s.Name); other >= 0 && other != idx {
		if b.Savestates[other].Name == s.Name {
			return fmt.Errorf("this bundle already has a savestate named %q", s.Name)
		}
		return fmt.Errorf("%s and %s would be the same file for the game (%s): pick another name",
			s.Name, b.Savestates[other].Name, savestate.FileName(s.Name))
	}
	if idx >= 0 {
		b.Savestates[idx] = s
	} else {
		b.Savestates = append(b.Savestates, s)
	}
	return nil
}

func (b *Bundle) Remove(name string) bool {
	i := b.Find(name)
	if i < 0 {
		return false
	}
	b.Savestates = append(b.Savestates[:i], b.Savestates[i+1:]...)
	return true
}

// OtherBundlesUsing lists the other bundles holding a profile with this name
// (they would write the same file).
func (s Store) OtherBundlesUsing(profile, except string) []string {
	names, _ := s.List()
	var out []string
	for _, n := range names {
		if strings.EqualFold(n, except) {
			continue
		}
		if b, err := s.Load(n); err == nil && b.Find(profile) >= 0 {
			out = append(out, n)
		}
	}
	return out
}
