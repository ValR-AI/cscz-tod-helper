package web

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"todhelper/internal/savestate"
)

// activityLog records every change the helper makes in the Save folder, one
// line per action, in <data>/logs/helper-YYYY-MM-DD.log. When the game does
// something odd, this tells what the helper did at that moment.
type activityLog struct {
	dir string
	mu  sync.Mutex
}

func (l *activityLog) add(action, format string, args ...any) {
	now := time.Now()
	line := fmt.Sprintf("%s  %-8s %s\n", now.Format("2006-01-02 15:04:05.000"), action, fmt.Sprintf(format, args...))
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(l.dir, "helper-"+now.Format("2006-01-02")+".log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line)
}

// install writes a savestate's profile and logs it, with what the file was
// before: new, identical, or replaced (and how many maps the replaced file had).
func (a *App) install(saveDir string, s savestate.Savestate, why string) error {
	path := savestate.Path(saveDir, s.Name)
	before := "new file"
	if old, err := os.ReadFile(path); err == nil {
		before = "replaced a different file"
		if want, err := savestate.Bytes(s); err == nil && bytes.Equal(old, want) {
			before = "same content as before"
		} else if st := savestate.FileStatus(saveDir, s); st.State == "changed" {
			before = fmt.Sprintf("replaced a file with %d maps won", st.Beaten)
		}
	}
	if err := savestate.Install(saveDir, s); err != nil {
		a.log.add("ERROR", "writing %s (%s): %v", savestate.FileName(s.Name), why, err)
		return err
	}
	a.log.add("WRITE", "%s (%s) [%s; %s]", savestate.FileName(s.Name), savestate.Describe(s), why, before)
	return nil
}

// remove deletes a profile and logs it.
func (a *App) remove(saveDir, name, why string) error {
	path := savestate.Path(saveDir, name)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if err := savestate.Remove(saveDir, name); err != nil {
		a.log.add("ERROR", "deleting %s (%s): %v", savestate.FileName(name), why, err)
		return err
	}
	a.log.add("DELETE", "%s [%s]", savestate.FileName(name), why)
	return nil
}
