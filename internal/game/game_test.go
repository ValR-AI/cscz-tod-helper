package game

import (
	"reflect"
	"testing"
)

func names(lineup []LineupEntry, present bool) []string {
	var out []string
	for _, e := range lineup {
		if e.Present == present && !e.Repeated {
			out = append(out, e.Name)
		}
	}
	return out
}

func TestLineup(t *testing.T) {
	tides := MapIndex("de_tides_cz")
	l := Lineup(0, tides, 7) // Easy, minEnemies 5, threshold 7
	if got := names(l, true); !reflect.DeepEqual(got, []string{"Weasel", "Lunatic", "Crackpot", "Madman", "Rat"}) {
		t.Errorf("Easy Tides at 7 points: %v", got)
	}
	if l[5].Name != "Screech" || l[5].JoinsAt != 8 || l[5].Present {
		t.Errorf("Screech joins at 8 points: %+v", l[5])
	}
	if Enemies(0, tides, 8) != 6 || Enemies(0, tides, 30) != 6 {
		t.Error("Easy Tides has 6 terrorists at most")
	}

	// Expert Airstrip lists Wolfhound twice: the second one cannot join.
	air := MapIndex("de_airstrip_cz")
	l = Lineup(3, air, 30)
	if !l[7].Repeated || l[7].Present || Enemies(3, air, 30) != 7 {
		t.Errorf("Expert Airstrip: %+v, %d enemies", l[7], Enemies(3, air, 30))
	}
}

func TestBuyable(t *testing.T) {
	cases := []struct {
		weapons []string
		ct      bool
		want    string
	}{
		{[]string{"ak47", "m4a1", "galil"}, true, "m4a1"}, // Maverick as a CT
		{[]string{"m4a1", "elites"}, false, "elites"},     // Lunatic: the game's own comment says he ends up with pistols
		{[]string{"m4a1", "ak47", "famas"}, false, "ak47"},
		{[]string{"shield", "m4a1"}, true, "shield"},
		{[]string{"aug", "sg552"}, false, "sg552"},
		{[]string{"tmp", "mac10"}, false, "mac10"},
		{nil, true, ""},
	}
	for _, c := range cases {
		if got := FirstBuyable(c.weapons, c.ct); got != c.want {
			t.Errorf("%v ct=%v: %q, want %q", c.weapons, c.ct, got, c.want)
		}
	}
}

func TestTerrorists(t *testing.T) {
	ts := Terrorists()
	if len(ts) != 32 || ts[0].Name != "Weasel" || ts[len(ts)-1].Name != "Snake" {
		t.Fatalf("%d terrorists, %s ... %s", len(ts), ts[0].Name, ts[len(ts)-1].Name)
	}
	w, _ := FindTerrorist("weasel")
	if w.Level != "Easy" || w.Skill != 0 || w.Aggression != 0 || w.Teamwork != 75 || w.ReactionTime != 0.5 || w.AttackDelay != 1.5 || w.Skin != 3 {
		t.Errorf("Weasel: %+v", w)
	}
	if len(FacedOn("Hammer")) != 0 || !reflect.DeepEqual(FacedOn("Weasel"), []int{0}) || !reflect.DeepEqual(FacedOn("Rebel"), []int{1, 2, 3}) {
		t.Errorf("faced on: Hammer %v, Weasel %v, Rebel %v", FacedOn("Hammer"), FacedOn("Weasel"), FacedOn("Rebel"))
	}
	for d := range mapRules {
		for i, r := range mapRules[d] {
			for _, b := range r.Bots {
				if _, ok := FindTerrorist(b); !ok {
					t.Errorf("%s %s: %s is not a terrorist", DifficultyNames[d], mapIDs[i], b)
				}
			}
		}
	}
}
