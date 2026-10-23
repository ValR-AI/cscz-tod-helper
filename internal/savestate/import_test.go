package savestate

import (
	"strings"
	"testing"

	"todhelper/internal/czp"
	"todhelper/internal/game"
)

func TestGameFileName(t *testing.T) {
	cases := map[string]string{
		"ValR_ToD1(U)":   "CZProfile-ValR_ToD1_U_.czp", // what the game wrote for the user's own test
		"ValR66_ToD0-U":  "CZProfile-ValR66_ToD0-U.czp",
		"_-()[].!@#$^&*": "CZProfile-_-____________.czp",
		"123456789012345678901234567890123456789012345678901234567890123": "CZProfile-1234567890123456789012345678901.czp",
	}
	for name, want := range cases {
		if got := FileName(name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if !SameFile("ToD1(U)", "tod1_u_") || SameFile("ToD1-U", "ToD1_U") {
		t.Error("SameFile")
	}
	for _, bad := range []string{"a b", `a"b`, `a\b`, "a%b", "a;b", "é"} {
		if CheckName(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if err := CheckName("_-()[].!@#$^&*{}|:'<,>"); err != nil {
		t.Errorf("symbols: %v", err)
	}
}

func TestFromProfile(t *testing.T) {
	// A helper profile comes back identical.
	in := Savestate{Name: "Repo_Italy", Difficulty: "easy", Beaten: maps("dust", "office", "prodigy"), Current: "cs_italy_cz",
		Team: []string{"Floyd", "Morris"}, ReadOnly: true}
	p, err := Profile(in)
	if err != nil {
		t.Fatal(err)
	}
	s, notices := FromProfile(p, true)
	want, _ := Bytes(in)
	got, _ := Bytes(s)
	if string(want) != string(got) || len(notices) != 0 || !s.ReadOnly {
		t.Fatalf("round trip: %+v %v", s, notices)
	}

	// A full save is recognized.
	full, _ := Profile(Savestate{Name: "All", Difficulty: "hard", AllDifficulties: true})
	if s, _ := FromProfile(full, false); !s.AllDifficulties || s.Difficulty != "hard" {
		t.Errorf("full save: %+v", s)
	}

	// A fresh profile made in the game (no career) and one with two careers.
	fresh := &czp.Profile{Name: "Fresh", Difficulty: 244, Campaigns: make([]*czp.Campaign, 4)}
	if s, n := FromProfile(fresh, false); s.Difficulty != "easy" || len(n) != 1 {
		t.Errorf("fresh: %+v %v", s, n)
	}
	two, _ := Profile(Savestate{Name: "Two", Difficulty: "easy", Beaten: maps("dust")})
	hard, _ := Profile(Savestate{Name: "Two", Difficulty: "hard"})
	two.Campaigns[2] = hard.Campaigns[2]
	two.Medals[0] = 1
	if s, n := FromProfile(two, false); s.Difficulty != "easy" || len(n) != 2 || !strings.Contains(n[0], "Hard (0 won)") {
		t.Errorf("two careers: %+v %v", s, n)
	}
}

func TestToDPack(t *testing.T) {
	pack, err := ToDPack("ValR", "easy")
	if err != nil || len(pack) != 12 {
		t.Fatalf("%v %d", err, len(pack))
	}
	if pack[0].Name != "ValR_ToD1-U" || pack[0].ReadOnly || pack[1].Name != "ValR_ToD1-L" || !pack[1].ReadOnly {
		t.Errorf("tour 1: %+v %+v", pack[0], pack[1])
	}
	t6 := pack[11]
	ev := Evaluate(t6)
	if t6.Name != "ValR_ToD6-L" || ev.BeatenCount != 15 || t6.Current != "de_piranesi_cz" || len(ev.Notices) != 0 || len(t6.Team) != 0 {
		t.Errorf("tour 6: %+v %v", t6, ev.Notices)
	}
	if ev.Points != game.StartPoints(0)+15 {
		t.Errorf("tour 6 points: %d", ev.Points)
	}
	if _, err := ToDPack("123456789012345678901234X", "easy"); err == nil {
		t.Error("25-character pseudo accepted")
	}
	if _, err := ToDPack("Val R", "easy"); err == nil {
		t.Error("space accepted")
	}
}
