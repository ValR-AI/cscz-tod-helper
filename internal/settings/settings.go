// Package settings keeps the helper's own configuration (config.json in its
// data folder).
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

const DefaultLiveSplitAddr = "127.0.0.1:16834"

// Timers the autosplitter can drive.
const (
	TimerLiveSplit    = "livesplit"     // LiveSplit (Windows), TCP server
	TimerLiveSplitOne = "livesplit-one" // LiveSplit One, "Connect to Server"
)

// defaultTimer: LiveSplit on Windows, LiveSplit One elsewhere.
func defaultTimer() string {
	if runtime.GOOS == "windows" {
		return TimerLiveSplit
	}
	return TimerLiveSplitOne
}

type Settings struct {
	SaveDir       string `json:"saveDir"`       // the game's Save folder, set by the user
	ActiveBundle  string `json:"activeBundle"`  // bundle shown in the builder
	LiveSplitAddr string `json:"liveSplitAddr"` // LiveSplit TCP server
	TimerKind     string `json:"timerKind"`     // livesplit or livesplit-one
	WatchFile     string `json:"watchFile"`     // profile file watched by the autosplitter (CZProfile-*.czp)
	WatchEnabled  bool   `json:"watchEnabled"`
}

func Load(path string) (Settings, error) {
	s := Settings{LiveSplitAddr: DefaultLiveSplitAddr, TimerKind: defaultTimer()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{LiveSplitAddr: DefaultLiveSplitAddr, TimerKind: defaultTimer()}, err
	}
	if s.LiveSplitAddr == "" {
		s.LiveSplitAddr = DefaultLiveSplitAddr
	}
	if s.TimerKind != TimerLiveSplit && s.TimerKind != TimerLiveSplitOne {
		s.TimerKind = defaultTimer()
	}
	return s, nil
}

func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
