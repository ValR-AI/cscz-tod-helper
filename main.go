// CS:CZ ToD Helper: savestate builder, bundle reset and autosplitter for
// Counter-Strike: Condition Zero career runs.
//
// Without arguments it starts a small web server on 127.0.0.1 and opens the
// interface in the browser. With arguments it is a command-line tool.
//
// The helper only reads and writes CZProfile-*.czp files in the Save folder
// the user picks. It never reads the game's memory or touches its process.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"todhelper/internal/cli"
	"todhelper/internal/web"
)

const Version = "0.4.0"

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "gui" {
		os.Exit(cli.Run(args, Version))
	}
	if len(args) > 0 {
		args = args[1:]
	}
	if err := runGUI(args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if runtime.GOOS == "windows" {
			fmt.Print("\nPress Enter to close.")
			bufio.NewReader(os.Stdin).ReadString('\n')
		}
		os.Exit(1)
	}
}

func runGUI(args []string) error {
	fs := flag.NewFlagSet("gui", flag.ContinueOnError)
	port := fs.Int("port", web.DefaultPort, "port of the local interface (0 = any free port)")
	data := fs.String("data", "", "folder for the helper's bundles, settings and logs")
	noBrowser := fs.Bool("no-browser", false, "do not open the browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dataDir := *data
	if dataDir == "" {
		var err error
		if dataDir, err = defaultDataDir(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	ln, actualPort, other, err := web.Listen(*port)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)
	if other {
		fmt.Println("CS:CZ ToD Helper is already running:", url)
		if !*noBrowser {
			web.OpenBrowser(url)
		}
		return nil
	}

	app, err := web.NewApp(Version, dataDir, actualPort)
	if err != nil {
		return err
	}
	defer app.Close()
	srv := &http.Server{Handler: app.Handler(actualPort), ReadHeaderTimeout: 10 * time.Second}

	fmt.Printf("CS:CZ ToD Helper %s\n\n", Version)
	fmt.Printf("  Interface : %s\n", url)
	fmt.Printf("  Data      : %s\n\n", dataDir)
	fmt.Println("Keep this window open while you use the helper (the autosplitter runs here).")
	fmt.Println("Close it or press Ctrl+C to quit.")
	if !*noBrowser {
		if err := web.OpenBrowser(url); err != nil {
			fmt.Println("Open this address in your browser:", url)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-stop:
		fmt.Println("\nStopping.")
		srv.Close()
		return nil
	case err := <-errc:
		return err
	}
}

// defaultDataDir keeps the helper portable: its data sits next to the
// program, unless that folder is not writable.
func defaultDataDir() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		dir := filepath.Join(filepath.Dir(exe), "cscz-tod-helper-data")
		if writable(filepath.Dir(exe)) {
			return dir, nil
		}
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "cscz-tod-helper"), nil
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".cscz-tod-helper-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
