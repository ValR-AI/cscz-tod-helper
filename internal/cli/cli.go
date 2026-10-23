// Package cli is the command-line version of the savestate builder, mostly
// useful for debugging and scripts. The same rules as the interface apply.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"todhelper/internal/autosplit"
	"todhelper/internal/czp"
	"todhelper/internal/game"
	"todhelper/internal/savestate"
	"todhelper/internal/settings"
)

const usage = `cscz-tod-helper %s - career savestates and autosplitter for Counter-Strike: Condition Zero

Run it without arguments to open the interface in your browser.
The command line below is mainly for debugging and scripts.

Usage:
  cscz-tod-helper                          open the interface
  cscz-tod-helper gui [--port N] [--data DIR] [--no-browser]
  cscz-tod-helper -n NAME [options]        write a savestate

Profile:
  -n, --name NAME          profile name (= in-game player name)
  -d, --difficulty LEVEL   easy, normal (or medium), hard, expert  [easy]

Progression (pick one; none = fresh career):
      --il MAP             every tour before MAP's tour won, MAP preselected
      --tours N            first N tours won (0-6)
      --beaten MAPS        comma-separated won maps, e.g. dust,office,prodigy,italy
      --complete           every map of all four difficulties won
      --current-map MAP    preselected map [default: IL map, else first map not won]

Team:
      --team NAMES         comma-separated teammates already hired
      --cheat              allow teammates from tiers not unlocked yet

Where (default: the folder this program is in):
      --folder DIR         the game's Save folder (or any folder)

Other:
      --writable           do not make the profile read-only
      --info [FILE...]     list the profiles of the folder, or show NAME / FILE
      --unlock             make profile NAME writable again
      --watch              print every save of profile NAME and split in LiveSplit
      --livesplit ADDR     LiveSplit TCP server for --watch [127.0.0.1:16834]
  -h, --help               show this help
      --version            show the version

Rules applied: a map counts as won only once every earlier tour is won; the
preselected map must be in an unlocked tour; points = difficulty start + 1 per
won map; up to min(points, 5) teammates whose total cost fits in the points,
from unlocked tiers unless --cheat.

Examples:
  cscz-tod-helper -n Repo_Italy --tours 1 --current-map italy --team floyd,morris,quincy,cooper,vern --folder "<Save>"
  cscz-tod-helper -n Repo_All --complete --folder "<Save>"
  cscz-tod-helper --info --folder "<Save>"
  cscz-tod-helper -n Repo_Italy --watch --folder "<Save>"
`

type options struct {
	name, difficulty        string
	il, beaten, currentMap  string
	team, folder, livesplit string
	tours                   int
	toursSet, complete      bool
	writable, cheat         bool
	info, unlock, watch     bool
	infoFiles               []string
	help, version           bool
}

func parse(args []string) (*options, error) {
	o := &options{difficulty: "easy", livesplit: settings.DefaultLiveSplitAddr}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, hasVal := arg, "", false
		if strings.HasPrefix(arg, "--") {
			if k, v, ok := strings.Cut(arg, "="); ok {
				key, val, hasVal = k, v, true
			}
		}
		value := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value (see --help)", key)
			}
			i++
			return args[i], nil
		}
		flag := func(dst *bool) error {
			if hasVal {
				return fmt.Errorf("%s takes no value", key)
			}
			*dst = true
			return nil
		}
		var err error
		switch key {
		case "-n", "--name":
			o.name, err = value()
		case "-d", "--difficulty":
			o.difficulty, err = value()
		case "--il":
			o.il, err = value()
		case "--beaten":
			o.beaten, err = value()
		case "--tours":
			var s string
			if s, err = value(); err == nil {
				if o.tours, err = strconv.Atoi(s); err != nil {
					err = fmt.Errorf("--tours needs a number, got %q", s)
				}
				o.toursSet = true
			}
		case "--complete":
			err = flag(&o.complete)
		case "--current-map":
			o.currentMap, err = value()
		case "--team":
			o.team, err = value()
		case "--cheat":
			err = flag(&o.cheat)
		case "--folder":
			o.folder, err = value()
		case "--writable":
			err = flag(&o.writable)
		case "--info":
			err = flag(&o.info)
		case "--unlock":
			err = flag(&o.unlock)
		case "--watch":
			err = flag(&o.watch)
		case "--livesplit":
			o.livesplit, err = value()
		case "-h", "--help", "-help", "/?":
			o.help = true
		case "--version", "-version":
			o.version = true
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown option %s (see --help)", arg)
			}
			if o.info {
				o.infoFiles = append(o.infoFiles, arg)
				continue
			}
			return nil, fmt.Errorf("unexpected argument %q (see --help)", arg)
		}
		if err != nil {
			return nil, err
		}
	}
	n := 0
	for _, set := range []bool{o.il != "", o.beaten != "", o.toursSet, o.complete} {
		if set {
			n++
		}
	}
	if n > 1 {
		return nil, errors.New("use only one of --il, --tours, --beaten and --complete")
	}
	return o, nil
}

func programFolder() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe), nil
}

func folder(o *options) (string, error) {
	if o.folder == "" {
		return programFolder()
	}
	dir := o.folder
	if home, err := os.UserHomeDir(); err == nil && (dir == "~" || strings.HasPrefix(dir, "~/")) {
		dir = filepath.Join(home, dir[1:])
	}
	return dir, savestate.CheckSaveDir(dir)
}

// Run executes the command line and returns the exit code.
func Run(args []string, version string) int {
	if err := run(args, version); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

func run(args []string, version string) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	switch {
	case o.help:
		fmt.Printf(usage, version)
		return nil
	case o.version:
		fmt.Println("cscz-tod-helper " + version)
		return nil
	case o.info:
		return info(o)
	}
	dir, err := folder(o)
	if err != nil {
		return err
	}
	if o.name == "" {
		return errors.New("give the profile name with --name (or run without arguments for the interface)")
	}
	if err := savestate.CheckName(o.name); err != nil {
		return err
	}
	switch {
	case o.unlock:
		path := savestate.Path(dir, o.name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("profile not found: %s", path)
		}
		if err := savestate.WriteFile(path, data, false); err != nil {
			return err
		}
		fmt.Printf("Profile is writable again: %s\n", path)
		return nil
	case o.watch:
		return watch(o, dir)
	}
	return generate(o, dir)
}

func generate(o *options, dir string) error {
	diff, err := game.FindDifficulty(o.difficulty)
	if err != nil {
		return err
	}
	s := savestate.Savestate{Name: o.name, Difficulty: game.DifficultyNames[diff], AllDifficulties: o.complete,
		ReadOnly: !o.writable, Cheat: o.cheat}
	maps := game.Maps()
	switch {
	case o.il != "":
		i, err := game.FindMap(o.il)
		if err != nil {
			return err
		}
		s.Beaten = maps[:game.TourOf(i)*game.MapsPerTour]
		s.Current = maps[i]
	case o.toursSet:
		if o.tours < 0 || o.tours > game.TourCount {
			return fmt.Errorf("--tours must be between 0 and %d", game.TourCount)
		}
		s.Beaten = maps[:o.tours*game.MapsPerTour]
	case o.beaten != "":
		for _, part := range strings.Split(o.beaten, ",") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			i, err := game.FindMap(part)
			if err != nil {
				return err
			}
			s.Beaten = append(s.Beaten, maps[i])
		}
	}
	if o.currentMap != "" {
		i, err := game.FindMap(o.currentMap)
		if err != nil {
			return err
		}
		s.Current = maps[i]
	}
	if o.team != "" {
		for _, part := range strings.Split(o.team, ",") {
			if part = strings.TrimSpace(part); part == "" {
				continue
			}
			if _, ok := game.FindTeammate(part); !ok {
				return fmt.Errorf("unknown teammate %q", part)
			}
			s.Team = append(s.Team, part)
		}
	}
	ev := savestate.Evaluate(s)
	if len(ev.Problems) > 0 {
		return errors.New(strings.Join(ev.Problems, "; "))
	}
	if err := savestate.Install(dir, ev.Savestate); err != nil {
		return err
	}
	path := savestate.Path(dir, ev.Savestate.Name)
	data, _ := os.ReadFile(path)
	p, err := czp.Parse(data)
	if err != nil {
		return err
	}
	fmt.Println("Savestate written.")
	show(p, path)
	for _, n := range ev.Notices {
		fmt.Println("  Note:", n)
	}
	return nil
}

func info(o *options) error {
	if len(o.infoFiles) > 0 {
		for _, f := range o.infoFiles {
			data, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			p, err := czp.Parse(data)
			if err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
			show(p, f)
		}
		return nil
	}
	dir, err := folder(o)
	if err != nil {
		return err
	}
	if o.name != "" {
		path := savestate.Path(dir, o.name)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		p, err := czp.Parse(data)
		if err != nil {
			return err
		}
		show(p, path)
		return nil
	}
	list, err := savestate.ListProfiles(dir)
	if err != nil {
		return err
	}
	fmt.Printf("Profiles in %s:\n", dir)
	if len(list) == 0 {
		fmt.Println("  (none)")
	}
	for _, p := range list {
		state := "writable"
		if p.ReadOnly {
			state = "read-only"
		}
		if !p.Valid {
			state += ", INVALID CHECKSUM"
		}
		fmt.Printf("  %-34s %3d maps won  [%s]\n", p.File, p.Beaten, state)
	}
	return nil
}

func show(p *czp.Profile, path string) {
	fmt.Printf("  Player      : %s\n", p.Name)
	if path != "" {
		state := "writable, the game can overwrite it"
		if savestate.IsReadOnly(path) {
			state = "read-only"
		}
		fmt.Printf("  File        : %s [%s]\n", path, state)
	}
	if p.ChecksumOK {
		fmt.Println("  Checksum    : valid")
	} else {
		fmt.Println("  Checksum    : INVALID, the game may reject this profile")
	}
	diff := fmt.Sprintf("? (%d)", p.Difficulty)
	if p.Difficulty < len(game.DifficultyNames) {
		diff = game.DifficultyNames[p.Difficulty]
	}
	fmt.Printf("  Difficulty  : %s\n", diff)
	started := false
	for d, c := range p.Campaigns {
		if c == nil || d >= len(game.DifficultyNames) {
			continue
		}
		started = true
		team := "empty"
		if len(c.Team) > 0 {
			team = strings.Join(c.Team, ", ")
		}
		fmt.Printf("  -- %s campaign: %d/%d maps won, %d points, team: %s\n",
			game.DifficultyNames[d], c.BeatenCount(), len(c.Maps), c.Points, team)
		for t := 0; t < len(c.Maps); t += 3 {
			var cells []string
			for _, m := range c.Maps[t:min(t+3, len(c.Maps))] {
				mark, cur := " ", " "
				if m.Beaten {
					mark = "x"
				}
				if m.Name == c.CurrentMap {
					cur = "<"
				}
				cells = append(cells, fmt.Sprintf("[%s] %-9s%s", mark, game.ShortName(m.Name), cur))
			}
			fmt.Printf("     Tour %d   %s\n", t/3+1, strings.Join(cells, "  "))
		}
		fmt.Printf("     (x = won, < = preselected: %s)\n", c.CurrentMap)
	}
	if !started {
		fmt.Println("  No campaign started")
	}
}

func watch(o *options, dir string) error {
	w := autosplit.NewWatcher(autosplit.NewLiveSplit(o.livesplit), "")
	w.Start(o.name, savestate.Path(dir, o.name))
	defer w.Stop()
	fmt.Printf("Watching %s (Ctrl+C to stop). Splits go to LiveSplit at %s.\n", savestate.Path(dir, o.name), o.livesplit)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	out := bufio.NewWriter(os.Stdout)
	var since int64
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-t.C:
			for _, e := range w.Status(since).Events {
				fmt.Fprintf(out, "%s  %-5s %s\n", e.Time.Format("15:04:05.000"), e.Kind, e.Text)
				since = e.ID
			}
			out.Flush()
		}
	}
}
