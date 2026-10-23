// Package savestate turns a savestate definition (what the user picks in the
// builder) into a valid career profile, enforces the game's rules, and
// writes the profile into the game's Save folder.
package savestate

import (
	"fmt"
	"strings"

	"todhelper/internal/czp"
	"todhelper/internal/game"
)

// Savestate is one entry of a bundle.
type Savestate struct {
	Name            string   `json:"name"`
	Difficulty      string   `json:"difficulty"`      // easy, normal, hard, expert
	AllDifficulties bool     `json:"allDifficulties"` // full save: every map of every difficulty beaten
	Beaten          []string `json:"beaten"`          // map ids, e.g. de_dust_cz
	Current         string   `json:"current"`         // preselected map id
	Team            []string `json:"team"`
	ReadOnly        bool     `json:"readOnly"`
	Cheat           bool     `json:"cheat"` // allow teammates from tiers that are not unlocked yet
}

const MaxNameLength = 31 // GoldSrc player name limit: the game also builds the file name from 31 characters

// NameSymbols are the characters a profile name may hold besides letters and
// digits. Left out: space, and " \ % ; which break the player name in the
// game (it refuses " and \ in a name, then saves the career under another
// name, so the profile splits in two).
const NameSymbols = "_-()[].!@#$^&*{}|:'<,>"

// CheckName keeps profile names to what the game handles as a player name.
func CheckName(name string) error {
	if name == "" {
		return fmt.Errorf("the profile name is empty")
	}
	if len(name) > MaxNameLength {
		return fmt.Errorf("the profile name is too long (%d characters max)", MaxNameLength)
	}
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(NameSymbols, r)
		if !ok {
			return fmt.Errorf("%q is not allowed in a profile name (letters, digits and %s)", r, strings.Join(strings.Split(NameSymbols, ""), " "))
		}
	}
	return nil
}

// fileNameChars are the characters the game keeps when it turns a profile
// name into a file name; any other character becomes '_'. Copied from the
// game (gameui.so, CCareerGame::SaveProfile and LoadProfile), which also only
// looks at the first 31 characters of the name.
const fileNameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_-0123456789"

// GameFileName is the part of the file name the game uses for a profile:
// ValR_ToD1(U) -> ValR_ToD1_U_.
func GameFileName(name string) string {
	b := []byte(name)
	if len(b) > MaxNameLength {
		b = b[:MaxNameLength]
	}
	for i, c := range b {
		if !strings.ContainsRune(fileNameChars, rune(c)) {
			b[i] = '_'
		}
	}
	return string(b)
}

// FileName is the profile's file in the Save folder, named the way the game
// looks for it: CZProfile-ValR_ToD1_U_.czp for ValR_ToD1(U).
func FileName(name string) string { return "CZProfile-" + GameFileName(name) + ".czp" }

// SameFile tells whether two profile names share one file (the game would
// mix them up). Windows does not tell upper and lower case apart.
func SameFile(a, b string) bool { return strings.EqualFold(GameFileName(a), GameFileName(b)) }

// Describe sums a savestate up for the helper's log, e.g.
// "Easy, 3/18 won, 5 points, team Floyd+Morris, start inferno, read-only".
func Describe(in Savestate) string {
	ev := Evaluate(in)
	s := ev.Savestate
	parts := []string{game.DifficultyNames[ev.DifficultyIndex], fmt.Sprintf("%d/%d won", ev.BeatenCount, game.MapCount())}
	if s.AllDifficulties {
		parts = []string{"full save, opens on " + game.DifficultyNames[ev.DifficultyIndex]}
	}
	parts = append(parts, fmt.Sprintf("%d points", ev.Points))
	if len(s.Team) > 0 {
		parts = append(parts, "team "+strings.Join(s.Team, "+"))
	} else {
		parts = append(parts, "no team")
	}
	parts = append(parts, "start "+game.ShortName(s.Current))
	if s.ReadOnly {
		parts = append(parts, "read-only")
	}
	if s.Cheat {
		parts = append(parts, "cheat")
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// Evaluation: normalized savestate plus everything the builder displays
// ---------------------------------------------------------------------------

type MapInfo struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Tour     int           `json:"tour"` // 1-6
	Beaten   bool          `json:"beaten"`
	Playable bool          `json:"playable"` // its tour is unlocked: can be beaten or preselected
	Current  bool          `json:"current"`
	Enemies  int           `json:"enemies"` // enemies if played with the savestate's points
	Lineup   []LineupEntry `json:"lineup"`  // the map's terrorists, in the order they join
	Tasks    []string      `json:"tasks"`
}

type LineupEntry struct {
	Name     string `json:"name"`
	JoinsAt  int    `json:"joinsAt"`  // points from which he plays (0: always)
	Present  bool   `json:"present"`  // plays with the savestate's points
	Repeated bool   `json:"repeated"` // listed twice in the game files: this copy never joins
}

type TeammateInfo struct {
	Name         string   `json:"name"`
	Cost         int      `json:"cost"`
	Skin         int      `json:"skin"`
	Model        string   `json:"model"`
	Skill        float64  `json:"skill"`
	Aggression   float64  `json:"aggression"`
	Teamwork     float64  `json:"teamwork"`
	ReactionTime float64  `json:"reactionTime"`
	AttackDelay  float64  `json:"attackDelay"`
	Weapons      []string `json:"weapons"` // readable names, in buying order
	Buyable      []bool   `json:"buyable"` // per weapon: a Counter-Terrorist can buy it
	Weapon       string   `json:"weapon"`  // first weapon a Counter-Terrorist can buy ("" if none)
	Selected     bool     `json:"selected"`
	Selectable   bool     `json:"selectable"` // can be added now (or removed, when selected)
	TierLocked   bool     `json:"tierLocked"` // tier not unlocked yet: only the cheat allows it
	Reason       string   `json:"reason"`     // why it cannot be added
}

// TerroristInfo is an enemy's sheet for the interface.
type TerroristInfo struct {
	Name         string   `json:"name"`
	Level        string   `json:"level"` // skill bracket: Easy, Fair, Normal, Tough, Hard, Very Hard, Expert, Elite
	Skin         int      `json:"skin"`
	Model        string   `json:"model"`
	Skill        float64  `json:"skill"`
	Aggression   float64  `json:"aggression"`
	Teamwork     float64  `json:"teamwork"`
	ReactionTime float64  `json:"reactionTime"`
	AttackDelay  float64  `json:"attackDelay"`
	Weapons      []string `json:"weapons"` // readable names, in buying order
	Buyable      []bool   `json:"buyable"` // per weapon: a terrorist can buy it
	Weapon       string   `json:"weapon"`  // first weapon a terrorist can buy ("" if none)
	FacedOn      []string `json:"facedOn"` // difficulties where he is on a map
}

// weapons turns weapon aliases into readable names, with which ones a side
// can buy and the first it can.
func weapons(aliases []string, ct bool) (names []string, buyable []bool, first string) {
	names, buyable = []string{}, []bool{}
	for _, w := range aliases {
		names = append(names, game.WeaponName(w))
		buyable = append(buyable, game.CanBuy(w, ct))
	}
	if f := game.FirstBuyable(aliases, ct); f != "" {
		first = game.WeaponName(f)
	}
	return names, buyable, first
}

// Terrorists returns every enemy's sheet, weakest bracket first.
func Terrorists() []TerroristInfo {
	out := []TerroristInfo{}
	for _, t := range game.Terrorists() {
		info := TerroristInfo{Name: t.Name, Level: game.LevelName(t.Level), Skin: t.Skin, Model: game.TModelNames[t.Skin],
			Skill: t.Skill, Aggression: t.Aggression, Teamwork: t.Teamwork, ReactionTime: t.ReactionTime,
			AttackDelay: t.AttackDelay, FacedOn: []string{}}
		info.Weapons, info.Buyable, info.Weapon = weapons(t.Weapons, false)
		for _, d := range game.FacedOn(t.Name) {
			info.FacedOn = append(info.FacedOn, game.DifficultyNames[d])
		}
		out = append(out, info)
	}
	return out
}

type TierInfo struct {
	Cost         int  `json:"cost"`
	UnlockPoints int  `json:"unlockPoints"`
	UnlockMaps   int  `json:"unlockMaps"` // beaten maps needed with the normal progression
	Unlocked     bool `json:"unlocked"`
	Never        bool `json:"never"` // cannot be reached on this difficulty
}

type Evaluation struct {
	Savestate       Savestate      `json:"savestate"` // normalized
	Notices         []string       `json:"notices"`   // what normalization changed
	Problems        []string       `json:"problems"`  // must be fixed before saving
	DifficultyIndex int            `json:"difficultyIndex"`
	Points          int            `json:"points"`
	MaxTeam         int            `json:"maxTeam"`
	TeamCost        int            `json:"teamCost"`
	TeamBudget      int            `json:"teamBudget"` // points the team must fit in (lowest difficulty on a full save)
	BeatenCount     int            `json:"beatenCount"`
	UnlockedTours   int            `json:"unlockedTours"`
	MatchWins       int            `json:"matchWins"`
	MatchWinBy      int            `json:"matchWinBy"`
	Maps            []MapInfo      `json:"maps"`
	Teammates       []TeammateInfo `json:"teammates"`
	Tiers           []TierInfo     `json:"tiers"`
	FileName        string         `json:"fileName"`
}

// limits describes what the team must respect. A full save must fit every
// difficulty, so it uses the most restrictive one.
type limits struct {
	points       int
	tierUnlocked func(cost int) bool
	tierReason   func(cost int) string
}

func teamLimits(diffs []int, beatenCount int) limits {
	points := 1 << 30
	for _, d := range diffs {
		points = min(points, game.Points(d, beatenCount))
	}
	return limits{
		points: points,
		tierUnlocked: func(cost int) bool {
			for _, d := range diffs {
				if !game.TierUnlocked(d, cost, game.Points(d, beatenCount)) {
					return false
				}
			}
			return true
		},
		tierReason: func(cost int) string {
			for _, d := range diffs {
				need := game.TierUnlockPoints(d, cost)
				if game.Points(d, beatenCount) < need {
					if need > game.MaxReachablePoints(d) {
						return fmt.Sprintf("Tier %d is never unlocked on %s", cost, game.DifficultyNames[d])
					}
					return fmt.Sprintf("Tier %d unlocks at %d points on %s", cost, need, game.DifficultyNames[d])
				}
			}
			return ""
		},
	}
}

// Evaluate normalizes a savestate so it respects the game's rules and
// returns what the builder needs to display.
func Evaluate(in Savestate) Evaluation {
	s := in
	ev := Evaluation{}
	notice := func(f string, a ...any) { ev.Notices = append(ev.Notices, fmt.Sprintf(f, a...)) }

	s.Name = strings.TrimSpace(s.Name)
	d, err := game.FindDifficulty(s.Difficulty)
	if err != nil {
		d = 0
		if s.Difficulty != "" {
			notice("Unknown difficulty %q, using Easy", s.Difficulty)
		}
	}
	s.Difficulty = strings.ToLower(game.DifficultyNames[d])
	ev.DifficultyIndex = d

	// Beaten maps.
	n := game.MapCount()
	beaten := make([]bool, n)
	for _, id := range s.Beaten {
		if i := game.MapIndex(id); i >= 0 {
			beaten[i] = true
		}
	}
	if s.AllDifficulties {
		for i := range beaten {
			beaten[i] = true
		}
	} else {
		norm := game.NormalizeBeaten(beaten)
		for i := range beaten {
			if beaten[i] && !norm[i] {
				notice("%s cannot be beaten yet: tour %d is locked", game.DisplayName(game.Maps()[i]), game.TourOf(i)+1)
			}
		}
		beaten = norm
	}
	s.Beaten = nil
	count := 0
	for i, b := range beaten {
		if b {
			s.Beaten = append(s.Beaten, game.Maps()[i])
			count++
		}
	}
	ev.BeatenCount = count
	ev.UnlockedTours = game.UnlockedTours(beaten)

	// Preselected map: any map of an unlocked tour.
	cur := game.MapIndex(s.Current)
	if cur < 0 || !game.MapPlayable(beaten, cur) {
		next := 0
		for i := range beaten {
			if !beaten[i] && game.MapPlayable(beaten, i) {
				next = i
				break
			}
		}
		if s.Current != "" {
			notice("%s is not playable with this progress, %s is preselected instead",
				game.DisplayName(s.Current), game.DisplayName(game.Maps()[next]))
		}
		cur = next
	}
	s.Current = game.Maps()[cur]

	// Team.
	diffs := []int{d}
	if s.AllDifficulties {
		diffs = []int{0, 1, 2, 3}
	}
	lim := teamLimits(diffs, count)
	ev.Points = game.Points(d, count)
	ev.MaxTeam = game.MaxTeam(lim.points)
	var team []string
	cost := 0
	seen := map[string]bool{}
	for _, name := range s.Team {
		t, ok := game.FindTeammate(name)
		if !ok {
			notice("Unknown teammate %q removed", name)
			continue
		}
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		switch {
		case !s.Cheat && !lim.tierUnlocked(t.Cost):
			notice("%s removed: %s", t.Name, lim.tierReason(t.Cost))
		case len(team) >= ev.MaxTeam:
			notice("%s removed: the team is full (%d slots)", t.Name, ev.MaxTeam)
		case cost+t.Cost > lim.points:
			notice("%s removed: not enough points (%d needed, %d left)", t.Name, t.Cost, lim.points-cost)
		default:
			team = append(team, t.Name)
			cost += t.Cost
		}
	}
	s.Team = team
	ev.TeamCost = cost
	ev.TeamBudget = lim.points

	for _, t := range game.Teammates() {
		info := TeammateInfo{Name: t.Name, Cost: t.Cost, Skin: t.Skin, Model: game.ModelNames[t.Skin],
			Skill: t.Skill, Aggression: t.Aggression, Teamwork: t.Teamwork, ReactionTime: t.ReactionTime,
			AttackDelay: t.AttackDelay, Selected: contains(team, t.Name)}
		info.Weapons, info.Buyable, info.Weapon = weapons(t.Weapons, true)
		info.TierLocked = !lim.tierUnlocked(t.Cost)
		switch {
		case info.Selected:
			info.Selectable = true
		case info.TierLocked && !s.Cheat:
			info.Reason = lim.tierReason(t.Cost)
		case len(team) >= ev.MaxTeam:
			info.Reason = fmt.Sprintf("The team is full (%d slots at %d points)", ev.MaxTeam, lim.points)
		case cost+t.Cost > lim.points:
			info.Reason = fmt.Sprintf("Not enough points: costs %d, %d left", t.Cost, lim.points-cost)
		default:
			info.Selectable = true
		}
		ev.Teammates = append(ev.Teammates, info)
	}
	for c := 1; c <= 5; c++ {
		need := game.TierUnlockPoints(d, c)
		ev.Tiers = append(ev.Tiers, TierInfo{
			Cost: c, UnlockPoints: need, UnlockMaps: max(0, need-game.StartPoints(d)),
			Unlocked: ev.Points >= need, Never: need > game.MaxReachablePoints(d),
		})
	}

	for i, id := range game.Maps() {
		lineup := []LineupEntry{}
		for _, e := range game.Lineup(d, i, ev.Points) {
			lineup = append(lineup, LineupEntry{Name: e.Name, JoinsAt: e.JoinsAt, Present: e.Present, Repeated: e.Repeated})
		}
		ev.Maps = append(ev.Maps, MapInfo{
			ID: id, Name: game.DisplayName(id), Tour: game.TourOf(i) + 1,
			Beaten: beaten[i], Playable: game.MapPlayable(beaten, i), Current: i == cur,
			Enemies: game.Enemies(d, i, ev.Points), Lineup: lineup, Tasks: taskTexts(game.Rule(d, i).Tasks),
		})
	}
	ev.MatchWins, ev.MatchWinBy = game.MatchWins(d), game.MatchWinBy(d)

	if err := CheckName(s.Name); err != nil {
		ev.Problems = append(ev.Problems, err.Error())
	}
	ev.FileName = FileName(s.Name)
	// Empty lists, never null, for the interface.
	if s.Beaten == nil {
		s.Beaten = []string{}
	}
	if s.Team == nil {
		s.Team = []string{}
	}
	if ev.Notices == nil {
		ev.Notices = []string{}
	}
	if ev.Problems == nil {
		ev.Problems = []string{}
	}
	ev.Savestate = s
	return ev
}

func taskTexts(tasks []string) []string {
	out := []string{}
	for _, t := range tasks {
		out = append(out, game.TaskText(t))
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Profile generation
// ---------------------------------------------------------------------------

// Profile builds the career profile of a savestate (normalized first).
func Profile(in Savestate) (*czp.Profile, error) {
	ev := Evaluate(in)
	if len(ev.Problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(ev.Problems, "; "))
	}
	s := ev.Savestate
	n := len(game.DifficultyNames)
	p := &czp.Profile{
		Name:       s.Name,
		Difficulty: ev.DifficultyIndex,
		Medals:     make([]byte, n),
		Campaigns:  make([]*czp.Campaign, n),
		Reserved:   make([]byte, czp.ReservedInts*4),
	}
	beaten := map[string]bool{}
	for _, id := range s.Beaten {
		beaten[id] = true
	}
	campaign := func(d int) *czp.Campaign {
		points := game.Points(d, len(s.Beaten))
		prev := points
		if len(s.Beaten) > 0 {
			prev = points - 1
		}
		c := &czp.Campaign{Points: points, Team: append([]string(nil), s.Team...), CurrentMap: s.Current, PrevPoints: prev}
		for _, id := range game.Maps() {
			c.Maps = append(c.Maps, czp.MapState{Name: id, Beaten: beaten[id]})
		}
		return c
	}
	if s.AllDifficulties {
		for d := 0; d < n; d++ {
			p.Campaigns[d] = campaign(d)
		}
	} else {
		p.Campaigns[ev.DifficultyIndex] = campaign(ev.DifficultyIndex)
	}
	return p, nil
}

// Bytes is the exact .czp file of a savestate.
func Bytes(s Savestate) ([]byte, error) {
	p, err := Profile(s)
	if err != nil {
		return nil, err
	}
	return p.Bytes()
}
