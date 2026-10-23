package savestate

import (
	"fmt"
	"strings"

	"todhelper/internal/czp"
	"todhelper/internal/game"
)

// FromProfile turns a career profile (made by the game, by the helper or by
// anyone) into a savestate: difficulty, won maps, preselected map, team and
// read-only flag. notices says what could not be kept.
func FromProfile(p *czp.Profile, readOnly bool) (Savestate, []string) {
	var notices []string
	s := Savestate{Name: p.Name, ReadOnly: readOnly, Beaten: []string{}, Team: []string{}}
	diffs := game.DifficultyNames
	var started []int
	for d, c := range p.Campaigns {
		if c != nil && d < len(diffs) {
			started = append(started, d)
		}
	}
	d := p.Difficulty
	if d < 0 || d >= len(diffs) {
		d = 0
	}
	if len(started) == 0 {
		s.Difficulty = strings.ToLower(diffs[d])
		notices = append(notices, fmt.Sprintf("No career started in this profile: imported as a fresh %s career.", diffs[d]))
		return s, notices
	}

	complete := len(started) == len(diffs)
	for _, i := range started {
		if p.Campaigns[i].BeatenCount() < game.MapCount() {
			complete = false
		}
	}
	if p.Campaigns[d] == nil {
		notices = append(notices, fmt.Sprintf("The profile opens on %s, which has no career: the %s career is used.", diffs[d], diffs[started[0]]))
		d = started[0]
	}
	c := p.Campaigns[d]
	s.Difficulty = strings.ToLower(diffs[d])
	s.Current = c.CurrentMap
	s.Team = append(s.Team, c.Team...)
	for _, m := range c.Maps {
		if m.Beaten {
			s.Beaten = append(s.Beaten, m.Name)
		}
	}
	if complete {
		s.AllDifficulties = true
	} else if len(started) > 1 {
		var others []string
		for _, i := range started {
			if i != d {
				others = append(others, fmt.Sprintf("%s (%d won)", diffs[i], p.Campaigns[i].BeatenCount()))
			}
		}
		notices = append(notices, fmt.Sprintf("Only the %s career is kept; the profile also has %s.", diffs[d], strings.Join(others, ", ")))
	}
	medals := false
	for _, m := range p.Medals {
		medals = medals || m != 0
	}
	if medals || p.Unknown != 0 {
		notices = append(notices, "Medals and the game's own counter are not kept: the helper writes them as 0.")
	}
	return s, append(notices, Evaluate(s).Notices...)
}

// PackSuffixes are the names of the recommended pack, after the pseudo:
// _ToD1-U, _ToD1-L ... _ToD6-L.
func PackSuffixes() []string {
	var out []string
	for t := 1; t <= game.TourCount; t++ {
		out = append(out, fmt.Sprintf("_ToD%d-U", t), fmt.Sprintf("_ToD%d-L", t))
	}
	return out
}

// MaxPseudoLength leaves room for "_ToD1-U" in a 31-character name.
const MaxPseudoLength = MaxNameLength - len("_ToD1-U")

// ToDPack builds the recommended pack: for each tour n, the career with the
// tours before n won and the first map of tour n preselected, no team.
// ToDn-U is writable (play the whole tour, the game records the wins);
// ToDn-L is read-only (win a map, reload, play it again).
func ToDPack(pseudo, difficulty string) ([]Savestate, error) {
	pseudo = strings.TrimSpace(pseudo)
	if pseudo == "" {
		return nil, fmt.Errorf("type a pseudo")
	}
	if len(pseudo) > MaxPseudoLength {
		return nil, fmt.Errorf("the pseudo is too long: %d characters max, so that the names stay within %d", MaxPseudoLength, MaxNameLength)
	}
	if err := CheckName(pseudo); err != nil {
		return nil, err
	}
	d, err := game.FindDifficulty(difficulty)
	if err != nil {
		return nil, err
	}
	maps := game.Maps()
	var out []Savestate
	for t := 0; t < game.TourCount; t++ {
		beaten := append([]string{}, maps[:t*game.MapsPerTour]...)
		for _, locked := range []bool{false, true} {
			suffix := "U"
			if locked {
				suffix = "L"
			}
			out = append(out, Savestate{
				Name:       fmt.Sprintf("%s_ToD%d-%s", pseudo, t+1, suffix),
				Difficulty: strings.ToLower(game.DifficultyNames[d]),
				Beaten:     beaten,
				Current:    maps[t*game.MapsPerTour],
				Team:       []string{},
				ReadOnly:   locked,
			})
		}
	}
	return out, nil
}
