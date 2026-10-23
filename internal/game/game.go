// Package game holds the career rules of Condition Zero (generated from the
// game's own files by tools/extract) and the checks built on them.
package game

import (
	"fmt"
	"strings"
)

type MapRule struct {
	MinEnemies int
	Threshold  int      // one enemy is added per reputation point above this
	Bots       []string // the map's terrorists, in the order they join
	Tasks      []string
}

// Teammate holds a hireable bot's values from BotCampaignProfile.db.
type Teammate struct {
	Name         string
	Cost         int
	Skin         int      // CT model: 0 random, 1 Urban, 2 GSG-9, 3 SAS, 4 GIGN, 5 Spetsnaz
	Skill        float64  // 0-100: aim and overall ability
	Aggression   float64  // 0-100: how readily it pushes and fights
	Teamwork     float64  // 0-100: how much it sticks with and listens to the team
	ReactionTime float64  // seconds before reacting to an enemy it sees
	AttackDelay  float64  // seconds before it starts shooting
	VoicePitch   int      // voice only
	Difficulty   []string // bot difficulty groups: only used for random bots, never in the career
	Weapons      []string // WeaponPreference: buys the first it can afford and that is allowed
}

// Terrorist holds an enemy bot's values from BotCampaignProfile.db.
type Terrorist struct {
	Name         string
	Level        string // skill bracket: the profile's first template (Easy, Fair, ... Elite)
	Skin         int    // T model: 0 random, 1 Phoenix, 2 L337 Krew, 3 Arctic, 4 Guerilla, 5 Militia
	Skill        float64
	Aggression   float64
	Teamwork     float64
	ReactionTime float64
	AttackDelay  float64
	Weapons      []string
}

// displayOrder is the order of the game's team screen, tier by tier.
var displayOrder = []string{
	"Stanley", "Gus", "Quincy", "Vern", "Floyd", "Morris", "Kenny", "Cooper",
	"Eddie", "Kent", "Ben", "Roger", "Phil", "Pete", "Oscar", "Leon",
	"Kirk", "Larry", "Cole", "Travis", "Ray", "Igor", "Sam", "Maverick",
	"Alex", "Ace", "Tex", "Jake", "Lou", "Rooster", "Brent",
	"Eagle", "Barney", "Bear", "Duke", "Sandman", "Steel",
}

// weaponNames turns the game's weapon aliases into readable names.
var weaponNames = map[string]string{
	"ak47": "AK-47", "m4a1": "M4A1", "famas": "FAMAS", "galil": "Galil", "aug": "AUG", "sg552": "SG 552",
	"awp": "AWP", "sg550": "SG 550", "g3sg1": "G3SG1", "scout": "Scout", "m249": "M249",
	"xm1014": "XM1014", "m3": "M3", "mp5": "MP5", "tmp": "TMP", "p90": "P90", "mac10": "MAC-10", "ump45": "UMP45",
	"deagle": "Desert Eagle", "fn57": "Five-SeveN", "p228": "228 Compact", "elites": "Dual Elites",
	"usp": "USP", "glock": "Glock", "shield": "Shield",
}

func WeaponName(alias string) string {
	if n, ok := weaponNames[strings.ToLower(alias)]; ok {
		return n
	}
	return strings.ToUpper(alias)
}

// Weapons each side can buy outside assassination maps (ReGameDLL
// weapontype.cpp, CanBuyWeaponByMaptype). A bot whose preferred weapon is
// for the other side fails to buy it and moves on to the next preference.
var ctCanBuy = map[string]bool{
	"p228": true, "scout": true, "xm1014": true, "aug": true, "fn57": true, "ump45": true, "sg550": true,
	"famas": true, "usp": true, "glock": true, "awp": true, "mp5": true, "m249": true, "m3": true,
	"m4a1": true, "tmp": true, "deagle": true, "p90": true, "shield": true,
}

var tCanBuy = map[string]bool{
	"p228": true, "scout": true, "xm1014": true, "mac10": true, "elites": true, "ump45": true, "galil": true,
	"usp": true, "glock": true, "awp": true, "mp5": true, "m249": true, "m3": true, "g3sg1": true,
	"deagle": true, "sg552": true, "ak47": true, "p90": true,
}

// CanBuy tells whether a side (ct true: Counter-Terrorists) can buy a weapon alias.
func CanBuy(alias string, ct bool) bool {
	a := strings.ToLower(alias)
	if ct {
		return ctCanBuy[a]
	}
	return tCanBuy[a]
}

// FirstBuyable is the first preferred weapon the side can buy ("" if none).
func FirstBuyable(weapons []string, ct bool) string {
	for _, w := range weapons {
		if CanBuy(w, ct) {
			return w
		}
	}
	return ""
}

const (
	TourCount   = 6
	MapsPerTour = 3
	MaxTeamSize = 5 // hire slots on the game's "Assemble team" screen
)

var DifficultyNames = []string{"Easy", "Normal", "Hard", "Expert"}

var ModelNames = []string{"Random", "Urban", "GSG-9", "SAS", "GIGN", "Spetsnaz"}

var TModelNames = []string{"Random", "Phoenix Connexion", "L337 Krew", "Arctic Avengers", "Guerilla Warfare", "Midwest Militia"}

// LevelName makes a template name readable: VeryHard -> Very Hard.
func LevelName(level string) string {
	if level == "VeryHard" {
		return "Very Hard"
	}
	return level
}

func Maps() []string { return append([]string(nil), mapIDs...) }

func MapCount() int { return len(mapIDs) }

// Teammates returns the hireable bots in the game's display order.
func Teammates() []Teammate {
	out := make([]Teammate, 0, len(teammates))
	used := map[string]bool{}
	for _, name := range displayOrder {
		for _, t := range teammates {
			if t.Name == name {
				out = append(out, t)
				used[name] = true
			}
		}
	}
	for _, t := range teammates { // any bot missing from displayOrder goes last
		if !used[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

func StartPoints(d int) int { return startPoints[d] }

func MatchWins(d int) int { return matchWins[d] }

func MatchWinBy(d int) int { return matchWinBy[d] }

func Rule(d, mapIndex int) MapRule { return mapRules[d][mapIndex] }

func TourOf(mapIndex int) int { return mapIndex / MapsPerTour }

func MapIndex(id string) int {
	for i, m := range mapIDs {
		if m == id {
			return i
		}
	}
	return -1
}

// ShortName turns de_dust_cz into dust.
func ShortName(id string) string {
	id = strings.TrimSuffix(id, "_cz")
	if strings.HasPrefix(id, "de_") || strings.HasPrefix(id, "cs_") {
		id = id[3:]
	}
	return id
}

// DisplayName turns de_dust2_cz into Dust2.
func DisplayName(id string) string {
	s := ShortName(id)
	if s == "" {
		return id
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// FindMap accepts dust, de_dust, de_dust_cz (any case).
func FindMap(s string) (int, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	for i, m := range mapIDs {
		if t == m || t == strings.TrimSuffix(m, "_cz") || t == ShortName(m) {
			return i, nil
		}
	}
	var names []string
	for _, m := range mapIDs {
		names = append(names, ShortName(m))
	}
	return -1, fmt.Errorf("unknown map %q (maps: %s)", s, strings.Join(names, ", "))
}

func FindDifficulty(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "easy", "e", "0":
		return 0, nil
	case "normal", "medium", "n", "m", "1":
		return 1, nil
	case "hard", "h", "2":
		return 2, nil
	case "expert", "x", "3":
		return 3, nil
	}
	return -1, fmt.Errorf("unknown difficulty %q (easy, normal, hard or expert)", s)
}

// Terrorists returns the enemy bots in the order of BotCampaignProfile.db
// (from the weakest bracket to the strongest).
func Terrorists() []Terrorist { return append([]Terrorist(nil), terrorists...) }

func FindTerrorist(name string) (Terrorist, bool) {
	for _, t := range terrorists {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Terrorist{}, false
}

// FacedOn lists the difficulties where a terrorist is on at least one map.
func FacedOn(name string) []int {
	var out []int
	for d := range mapRules {
		found := false
		for _, r := range mapRules[d] {
			for _, b := range r.Bots {
				if strings.EqualFold(b, name) {
					found = true
				}
			}
		}
		if found {
			out = append(out, d)
		}
	}
	return out
}

func FindTeammate(s string) (Teammate, bool) {
	for _, t := range teammates {
		if strings.EqualFold(t.Name, strings.TrimSpace(s)) {
			return t, true
		}
	}
	return Teammate{}, false
}

// ---------------------------------------------------------------------------
// Progression rules
// ---------------------------------------------------------------------------

// Points is the reputation of a campaign: the difficulty's start + 1 per beaten map.
func Points(d, beatenCount int) int { return startPoints[d] + beatenCount }

// CompletedTours counts the leading tours whose three maps are all beaten.
func CompletedTours(beaten []bool) int {
	for t := 0; t < TourCount; t++ {
		for i := t * MapsPerTour; i < (t+1)*MapsPerTour; i++ {
			if !beaten[i] {
				return t
			}
		}
	}
	return TourCount
}

// UnlockedTours is the number of tours you can play: the completed ones plus the next.
func UnlockedTours(beaten []bool) int {
	return min(CompletedTours(beaten)+1, TourCount)
}

// MapPlayable tells whether a map's tour is unlocked.
func MapPlayable(beaten []bool, mapIndex int) bool {
	return TourOf(mapIndex) < UnlockedTours(beaten)
}

// NormalizeBeaten drops beaten maps that sit in tours you could not have
// reached (a map only counts once every earlier tour is complete).
func NormalizeBeaten(beaten []bool) []bool {
	out := append([]bool(nil), beaten...)
	limit := CompletedTours(out)
	for i := range out {
		if TourOf(i) > limit {
			out[i] = false
		}
	}
	return out
}

// LineupEntry is one terrorist of a map's roster.
type LineupEntry struct {
	Name     string
	JoinsAt  int  // reputation points from which he plays (0: always)
	Present  bool // plays with the given points
	Repeated bool // listed again further down the roster: that copy never joins
}

// Lineup is a map's roster for a reputation score. The game takes the first
// MinEnemies names of the "bots" line, plus one more name per point above
// Threshold (CareerGame*.vdf). Bots are added by name, and a name already
// in the game is refused, so a name listed twice only plays once.
func Lineup(d, mapIndex, points int) []LineupEntry {
	r := mapRules[d][mapIndex]
	n := r.MinEnemies + max(0, points-r.Threshold)
	seen := map[string]bool{}
	var out []LineupEntry
	for i, name := range r.Bots {
		e := LineupEntry{Name: name}
		if i >= r.MinEnemies {
			e.JoinsAt = r.Threshold + i - r.MinEnemies + 1
		}
		key := strings.ToLower(name)
		e.Repeated = seen[key]
		seen[key] = true
		e.Present = i < n && !e.Repeated
		out = append(out, e)
	}
	return out
}

// Enemies is the number of terrorists on a map for a reputation score.
func Enemies(d, mapIndex, points int) int {
	n := 0
	for _, e := range Lineup(d, mapIndex, points) {
		if e.Present {
			n++
		}
	}
	return n
}

// MaxTeam is the number of teammates you can hire: one per point, up to 5.
func MaxTeam(points int) int { return min(points, MaxTeamSize) }

// TierUnlockPoints is the reputation needed to hire a teammate of a cost;
// 0 means available from the start, and a value above any reachable score
// means never.
func TierUnlockPoints(d, cost int) int {
	for c := 1; c <= cost && c < len(costAvailability[d]); c++ {
		if costAvailability[d][c] == 0 {
			return 0
		}
	}
	if cost < len(costAvailability[d]) {
		return costAvailability[d][cost]
	}
	return 0
}

func MaxReachablePoints(d int) int { return startPoints[d] + len(mapIDs) }

func TierUnlocked(d, cost, points int) bool { return points >= TierUnlockPoints(d, cost) }

// TaskText makes a CareerGame task readable, e.g. "killwith 2 sniper" ->
// "Kill 2 enemies with a sniper rifle".
func TaskText(task string) string {
	f := strings.Fields(task)
	if len(f) == 0 {
		return task
	}
	suffix := ""
	for len(f) > 1 && (f[len(f)-1] == "survive" || f[len(f)-1] == "inarow") {
		if f[len(f)-1] == "survive" {
			suffix = " and survive the round" + suffix
		} else {
			suffix = " in a row without dying" + suffix
		}
		f = f[:len(f)-1]
	}
	arg := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return "?"
	}
	plural := func(n, one, many string) string {
		if n == "1" {
			return n + " " + one
		}
		return n + " " + many
	}
	weapon := func(w string) string {
		switch strings.ToLower(w) {
		case "smg":
			return "an SMG"
		case "sniper":
			return "a sniper rifle"
		case "shotgun":
			return "a shotgun"
		case "pistol":
			return "a pistol"
		case "rifle":
			return "a rifle"
		case "machinegun":
			return "a machine gun"
		case "grenade", "hegren":
			return "grenades"
		case "knife":
			return "the knife"
		case "shield":
			return "the shield"
		default:
			return "the " + strings.ToUpper(w)
		}
	}
	var s string
	switch f[0] {
	case "kill":
		s = "Kill " + plural(arg(1), "enemy", "enemies")
	case "killwith":
		s = "Kill " + plural(arg(1), "enemy", "enemies") + " with " + weapon(arg(2))
	case "killblind":
		s = "Kill " + plural(arg(1), "blinded enemy", "blinded enemies")
	case "headshot":
		s = "Get " + plural(arg(1), "headshot", "headshots")
	case "headshotwith":
		s = "Get " + plural(arg(1), "headshot", "headshots") + " with " + weapon(arg(2))
	case "winfast":
		s = "Win a round in " + arg(1) + " seconds"
	case "rescue":
		s = "Rescue " + plural(arg(1), "hostage", "hostages")
	case "rescueall":
		s = "Rescue all hostages"
	case "killall":
		s = "Kill all enemies"
	case "injure":
		s = "Injure " + plural(arg(1), "enemy", "enemies")
	case "injurewith":
		s = "Injure " + plural(arg(1), "enemy", "enemies") + " with " + weapon(arg(2))
	default:
		return task
	}
	return s + suffix
}
