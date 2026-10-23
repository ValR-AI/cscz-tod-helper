//go:build !windows

package web

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// OpenBrowser opens url in the default browser (xdg-open on Linux, open on macOS).
func OpenBrowser(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}

// BrowseFolder shows a folder picker with zenity (GNOME and most desktops)
// or kdialog (KDE), when one of them is installed. It returns "" when the
// user cancels.
func BrowseFolder(title, start string) (string, error) {
	if start == "" {
		start = "."
	}
	tools := [][]string{
		{"zenity", "--file-selection", "--directory", "--title=" + title, "--filename=" + strings.TrimSuffix(start, "/") + "/"},
		{"kdialog", "--getexistingdirectory", start, "--title", title},
	}
	for _, t := range tools {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		out, err := exec.Command(t[0], t[1:]...).Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil // cancelled
		}
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(out), "\r\n"), nil
	}
	return "", errors.New("no folder picker found (install zenity or kdialog), paste the path instead")
}
