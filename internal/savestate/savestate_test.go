package savestate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"todhelper/internal/czp"
	"todhelper/internal/game"
)

func maps(names ...string) []string {
	var out []string
	for _, n := range names {
		i, err := game.FindMap(n)
		if err != nil {
			panic(err)
		}
		out = append(out, game.Maps()[i])
	}
	return out
}

// The real "inferno" profile written by the game, rebuilt from a definition,
// must come out byte for byte (set CZP_FIXTURES to a folder with inferno.czp).
func TestRebuildRealProfile(t *testing.T) {
	dir := os.Getenv("CZP_FIXTURES")
	if dir == "" {
		t.Skip("CZP_FIXTURES not set")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "inferno.czp"))
	if err != nil {
		t.Skip("inferno.czp not in CZP_FIXTURES")
	}
	s := Savestate{Name: "ValR66_inferno", Difficulty: "easy", Beaten: maps("dust", "office", "prodigy"),
		Current: "de_tides_cz", Team: []string{"kenny", "cooper", "morris", "floyd"}}
	data, err := Bytes(s)
	if err != nil || !bytes.Equal(data, raw) {
		t.Fatalf("rebuilt profile differs from the game's file (%v)", err)
	}
}

func TestBeatenNeedsEarlierTours(t *testing.T) {
	ev := Evaluate(Savestate{Name: "x", Beaten: maps("dust", "office", "tides")})
	if ev.BeatenCount != 2 || len(ev.Notices) != 1 || !strings.Contains(ev.Notices[0], "Tides") {
		t.Fatalf("tides should be dropped: %+v %v", ev.Savestate.Beaten, ev.Notices)
	}
	ev = Evaluate(Savestate{Name: "x", Beaten: maps("dust", "office", "prodigy", "italy")})
	if ev.BeatenCount != 4 || ev.UnlockedTours != 2 {
		t.Fatalf("italy is allowed once tour 1 is done: %+v", ev.Savestate.Beaten)
	}
}

func TestPreselectedMap(t *testing.T) {
	ev := Evaluate(Savestate{Name: "x", Beaten: maps("dust", "office", "prodigy"), Current: "de_torn_cz"})
	if ev.Savestate.Current != "de_tides_cz" {
		t.Errorf("a locked map must not stay preselected: %s", ev.Savestate.Current)
	}
	ev = Evaluate(Savestate{Name: "x", Beaten: maps("dust", "office", "prodigy"), Current: "de_dust_cz"})
	if ev.Savestate.Current != "de_dust_cz" {
		t.Errorf("a beaten map of an unlocked tour can be preselected: %s", ev.Savestate.Current)
	}
	ev = Evaluate(Savestate{Name: "x", AllDifficulties: true})
	if ev.Savestate.Current != "de_dust_cz" || ev.BeatenCount != 18 {
		t.Errorf("full save: dust by default, all beaten: %s %d", ev.Savestate.Current, ev.BeatenCount)
	}
}

func TestTeamRules(t *testing.T) {
	// Easy, no map beaten: 2 points, 2 slots, tier 1 only.
	ev := Evaluate(Savestate{Name: "x", Team: []string{"floyd", "morris", "quincy"}})
	if len(ev.Savestate.Team) != 2 || ev.MaxTeam != 2 {
		t.Errorf("2 slots at 2 points: %v", ev.Savestate.Team)
	}
	// Easy after 10 maps: 12 points, 5 slots, tiers 1-3.
	ten := maps("dust", "office", "prodigy", "tides", "italy", "inferno", "dust2", "militia", "stadium", "havana")
	ev = Evaluate(Savestate{Name: "x", Beaten: ten, Team: []string{"maverick", "larry", "sam", "ben", "morris"}})
	if len(ev.Savestate.Team) != 5 || ev.TeamCost != 12 || len(ev.Notices) != 0 {
		t.Errorf("Repo_Chateau team should fit exactly: %v %d %v", ev.Savestate.Team, ev.TeamCost, ev.Notices)
	}
	ev = Evaluate(Savestate{Name: "x", Beaten: ten, Team: []string{"tex"}})
	if len(ev.Savestate.Team) != 0 {
		t.Errorf("tier 4 needs 15 points on Easy")
	}
	ev = Evaluate(Savestate{Name: "x", Beaten: ten, Team: []string{"tex"}, Cheat: true})
	if len(ev.Savestate.Team) != 1 {
		t.Errorf("the cheat allows a locked tier")
	}
	ev = Evaluate(Savestate{Name: "x", Team: []string{"bear"}, Cheat: true})
	if len(ev.Savestate.Team) != 0 {
		t.Errorf("the cheat does not lift the points limit (bear costs 5, 2 points)")
	}
	// Expert: tiers 1-4 from the start.
	ev = Evaluate(Savestate{Name: "x", Difficulty: "expert", Team: []string{"tex", "ace"}})
	if len(ev.Savestate.Team) != 2 {
		t.Errorf("expert starts with 8 points and tier 4: %v %v", ev.Savestate.Team, ev.Notices)
	}
	var tex TeammateInfo
	for _, tm := range Evaluate(Savestate{Name: "x"}).Teammates {
		if tm.Name == "Tex" {
			tex = tm
		}
	}
	if tex.Selectable || !tex.TierLocked || !strings.Contains(tex.Reason, "15 points") {
		t.Errorf("Tex on a fresh Easy save: %+v", tex)
	}
}

func TestEnemiesAndTiers(t *testing.T) {
	ev := Evaluate(Savestate{Name: "x", Beaten: maps("dust")})
	if ev.Maps[1].Enemies != 4 || ev.Maps[5].Enemies != 5 {
		t.Errorf("easy at 3 points: office 4 enemies, inferno 5: %d %d", ev.Maps[1].Enemies, ev.Maps[5].Enemies)
	}
	if ev.Tiers[1].UnlockMaps != 4 || !ev.Tiers[4].Never {
		t.Errorf("easy tier 2 after 4 maps, tier 5 never: %+v", ev.Tiers)
	}
}

func TestBuildFullSave(t *testing.T) {
	data, err := Bytes(Savestate{Name: "All", Difficulty: "hard", AllDifficulties: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := czp.Parse(data)
	if err != nil || !p.ChecksumOK || p.Difficulty != 2 || p.BeatenTotal() != 72 {
		t.Fatalf("full save: %v", err)
	}
	for d, c := range p.Campaigns {
		if c.Points != game.StartPoints(d)+18 {
			t.Errorf("%s points %d", game.DifficultyNames[d], c.Points)
		}
	}
}

func TestInstallStatusRemove(t *testing.T) {
	dir := t.TempDir()
	s := Savestate{Name: "Repo_Italy", Beaten: maps("dust", "office", "prodigy"), Current: "cs_italy_cz",
		Team: []string{"floyd", "morris", "quincy", "cooper", "vern"}, ReadOnly: true}
	if FileStatus(dir, s).State != "missing" {
		t.Fatal("expected missing")
	}
	if err := Install(dir, s); err != nil {
		t.Fatal(err)
	}
	path := Path(dir, "Repo_Italy")
	if st := FileStatus(dir, s); st.State != "ready" || !st.ReadOnly || st.Beaten != 3 {
		t.Fatalf("after install: %+v", st)
	}
	// The game wins a map: simulate by writing another profile there.
	other, _ := Bytes(Savestate{Name: "Repo_Italy", Beaten: maps("dust", "office", "prodigy", "italy")})
	if err := WriteFile(path, other, false); err != nil {
		t.Fatal(err)
	}
	if st := FileStatus(dir, s); st.State != "changed" || st.Beaten != 4 {
		t.Fatalf("after a win: %+v", st)
	}
	if err := Install(dir, s); err != nil || FileStatus(dir, s).State != "ready" {
		t.Fatalf("reinstall: %v", err)
	}
	if err := Remove(dir, "Repo_Italy"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only profile should be deleted")
	}
	list, err := ListProfiles(dir)
	if err != nil || len(list) != 0 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func TestWriteFileLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	s := Savestate{Name: "Atomic", ReadOnly: true}
	for i := 0; i < 2; i++ { // the second write replaces a read-only profile
		if err := Install(dir, s); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "CZProfile-Atomic.czp" || !IsReadOnly(Path(dir, "Atomic")) {
		t.Fatalf("Save folder after two writes: %v", entries)
	}
	if got := Describe(Savestate{Name: "x", Beaten: maps("dust"), Team: []string{"vern"}, Current: "de_office_cz", ReadOnly: true}); got != "Easy, 1/18 won, 3 points, team Vern, start office, read-only" {
		t.Errorf("Describe: %q", got)
	}
}

func TestTerroristSheets(t *testing.T) {
	ts := Terrorists()
	for _, x := range ts {
		if len(x.Weapons) != len(x.Buyable) || x.FacedOn == nil {
			t.Fatalf("%s: %+v", x.Name, x)
		}
		switch x.Name {
		case "Lunatic":
			if x.Weapon != "Dual Elites" || x.Buyable[0] {
				t.Errorf("Lunatic cannot buy his M4A1: %+v", x)
			}
		case "Nails":
			if x.Level != "Very Hard" || x.Model != "Arctic Avengers" {
				t.Errorf("Nails: %+v", x)
			}
		}
	}
	ev := Evaluate(Savestate{Name: "x", Beaten: maps("dust", "office", "prodigy"), Current: "de_tides_cz"})
	tides := ev.Maps[3]
	if tides.Enemies != 5 || len(tides.Lineup) != 6 || tides.Lineup[5].Present || tides.Lineup[5].JoinsAt != 8 {
		t.Errorf("Tides at 5 points: %+v", tides.Lineup)
	}
}

func TestEvaluateNeverReturnsNullLists(t *testing.T) {
	ev := Evaluate(Savestate{})
	if ev.Notices == nil || ev.Problems == nil || ev.Savestate.Beaten == nil || ev.Savestate.Team == nil {
		t.Fatal("lists must be empty, not nil (the interface reads .length)")
	}
	for _, m := range ev.Maps {
		if m.Tasks == nil {
			t.Fatal("tasks must not be nil")
		}
	}
}

func TestTeammateData(t *testing.T) {
	ev := Evaluate(Savestate{Name: "x"})
	first, last := ev.Teammates[0], ev.Teammates[len(ev.Teammates)-1]
	if first.Name != "Stanley" || last.Name != "Steel" || ev.Teammates[7].Name != "Cooper" || ev.Teammates[8].Name != "Eddie" {
		t.Fatalf("display order: %s ... %s", first.Name, last.Name)
	}
	for _, tm := range ev.Teammates {
		switch tm.Name {
		case "Sam":
			if tm.Skin != 1 || tm.Model != "Urban" {
				t.Errorf("Sam is Urban in the game (\"SKin = 1\"), got skin %d", tm.Skin)
			}
		case "Maverick":
			if tm.Skill != 100 || tm.Aggression != 100 || tm.Teamwork != 0 || tm.ReactionTime != 0.2 || tm.Weapons[0] != "AK-47" ||
				tm.Weapon != "M4A1" || tm.Buyable[0] || !tm.Buyable[1] {
				t.Errorf("Maverick buys the AK-47 first but a CT cannot: %+v", tm)
			}
		case "Vern":
			if tm.Skill != 0 || tm.Teamwork != 100 || tm.AttackDelay != 1.5 || len(tm.Weapons) != 1 || tm.Weapons[0] != "MP5" {
				t.Errorf("Vern: %+v", tm)
			}
		case "Kirk":
			if tm.Skill != 50 || tm.Weapons[0] != "AWP" || tm.Cost != 3 {
				t.Errorf("Kirk (Normal+Sniper): %+v", tm)
			}
		}
	}
}
