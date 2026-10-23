// Command extract builds the game data used by CS:CZ ToD Helper from a
// Condition Zero install. It is a developer tool: run it once on your own
// copy of the game, then build the program. The helper itself never reads
// any game file except the CZProfile-*.czp profiles.
//
//	go run ./tools/extract -czero "/path/to/Half-Life/czero"
//
// It writes:
//   - internal/game/data_gen.go       rules from CareerGame*.vdf and BotCampaignProfile.db
//   - internal/web/ui/img/...         icons converted from the game's TGA files
//     (map thumbnails, CT and T model thumbnails per bot "Skin", tier badges, lock, logo)
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var difficulties = []string{"Easy", "Normal", "Hard", "Expert"}

// CT models in the order of the appearance menu (ReGameDLL client.cpp,
// HandleMenu_ChooseAppearance): bot profile "Skin = N" picks slot N.
var ctModels = []string{"ct_random", "urban", "gsg9", "sas", "gign", "spetsnaz"}

// T models, same menu: 5 is "militia" in Condition Zero.
var tModels = []string{"t_random", "terror", "leet", "arctic", "guerilla", "militia"}

func main() {
	czero := flag.String("czero", "", "the game's czero folder")
	out := flag.String("out", ".", "root of the cscz-tod-helper source tree")
	flag.Parse()
	if *czero == "" {
		log.Fatal("give the game's czero folder with -czero")
	}

	var gd gameData
	for d, name := range difficulties {
		root, err := parseKV(filepath.Join(*czero, "CareerGame"+name+".vdf"))
		if err != nil {
			log.Fatal(err)
		}
		gd.addDifficulty(d, root.child("CareerGame"))
	}
	bots, err := parseBotProfiles(filepath.Join(*czero, "BotCampaignProfile.db"))
	if err != nil {
		log.Fatal(err)
	}
	byName := map[string]botProfile{}
	for _, b := range bots {
		byName[b.name] = b
	}
	isTeammate := map[string]bool{}
	for _, name := range gd.characters {
		b, ok := byName[name]
		if !ok {
			log.Fatalf("teammate %s missing from BotCampaignProfile.db", name)
		}
		isTeammate[name] = true
		gd.teammates = append(gd.teammates, b)
	}
	// Every other profile is a terrorist, in the order of the file.
	for _, b := range bots {
		if !isTeammate[b.name] {
			gd.terrorists = append(gd.terrorists, b)
		}
	}
	for d := range difficulties {
		for i, r := range gd.rules[d] {
			for _, name := range r.bots {
				if b, ok := byName[name]; !ok || isTeammate[b.name] {
					log.Fatalf("%s %s: enemy %s is not a terrorist profile", difficulties[d], gd.maps[i], name)
				}
			}
		}
	}

	src, err := gd.goSource()
	if err != nil {
		log.Fatal(err)
	}
	genPath := filepath.Join(*out, "internal", "game", "data_gen.go")
	if err := os.WriteFile(genPath, src, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", genPath)

	img := filepath.Join(*out, "internal", "web", "ui", "img")
	conv := func(from, to string) {
		if err := tgaToPNG(filepath.Join(*czero, from), filepath.Join(img, to)); err != nil {
			log.Fatal(err)
		}
	}
	for _, m := range gd.maps {
		conv(filepath.Join("gfx", "thumbnails", "maps_wide", m+".tga"), filepath.Join("maps", m+".png"))
	}
	for i, m := range ctModels {
		conv(filepath.Join("gfx", "thumbnails", "skins", m+".tga"), filepath.Join("models", strconv.Itoa(i)+".png"))
	}
	for i, m := range tModels {
		conv(filepath.Join("gfx", "thumbnails", "skins", m+".tga"), filepath.Join("models", "t"+strconv.Itoa(i)+".png"))
	}
	for c := 1; c <= 5; c++ {
		conv(filepath.Join("gfx", "career", fmt.Sprintf("circled_%d_away_big.tga", c)), filepath.Join("tiers", strconv.Itoa(c)+".png"))
	}
	conv(filepath.Join("gfx", "career", "Lock.tga"), "lock.png")
	conv(filepath.Join("gfx", "career", "Logo_CSCZ_Big.tga"), "logo.png")
	fmt.Println("wrote icons to", img)
}

// ---------------------------------------------------------------------------
// Game data
// ---------------------------------------------------------------------------

type mapRule struct {
	min, threshold int
	bots           []string // enemy roster, left to right
	tasks          []string
}

// botProfile mirrors the attributes the game reads from BotCampaignProfile.db
// (ReGameDLL bot_profile.cpp: Aggression, Skill, Teamwork, Cost, Skin,
// VoicePitch, WeaponPreference, ReactionTime, AttackDelay, Difficulty).
type botProfile struct {
	name                        string
	level                       string  // first template of the profile line, e.g. "VeryHard"
	skill, aggression, teamwork float64 // 0-100 as written in the file
	reactionTime, attackDelay   float64 // seconds
	cost, skin, voicePitch      int
	difficulty                  []string // EASY, NORMAL, HARD, EXPERT
	weapons                     []string // WeaponPreference, in order
}

type gameData struct {
	maps        []string
	characters  []string
	startPoints [4]int
	matchWins   [4]int
	matchWinBy  [4]int
	costAvail   [4][6]int
	rules       [4][]mapRule
	teammates   []botProfile
	terrorists  []botProfile
}

var taskRe = regexp.MustCompile(`'([^']*)'`)

func (g *gameData) addDifficulty(d int, cg *kv) {
	atoi := func(key string) int {
		n, err := strconv.Atoi(cg.value(key))
		if err != nil {
			log.Fatalf("%s: bad %s", difficulties[d], key)
		}
		return n
	}
	g.startPoints[d] = atoi("InitialPoints")
	g.matchWins[d] = atoi("MatchWins")
	g.matchWinBy[d] = atoi("MatchWinBy")
	chars := strings.Fields(cg.value("Characters"))
	if g.characters == nil {
		g.characters = chars
	} else if strings.Join(chars, " ") != strings.Join(g.characters, " ") {
		log.Fatalf("%s: the teammate list differs between difficulties", difficulties[d])
	}
	if ca := cg.child("CostAvailability"); ca != nil {
		for _, e := range ca.items {
			cost, err1 := strconv.Atoi(e.key)
			pts, err2 := strconv.Atoi(e.val)
			if err1 == nil && err2 == nil && cost >= 1 && cost <= 5 {
				g.costAvail[d][cost] = pts
			}
		}
	}
	var maps []string
	for _, e := range cg.child("Maps").items {
		if e.sub == nil {
			continue
		}
		maps = append(maps, e.key)
		n := func(key string) int {
			v, err := strconv.Atoi(e.sub.value(key))
			if err != nil {
				log.Fatalf("%s %s: bad %s", difficulties[d], e.key, key)
			}
			return v
		}
		var tasks []string
		for _, m := range taskRe.FindAllStringSubmatch(e.sub.value("tasks"), -1) {
			tasks = append(tasks, m[1])
		}
		g.rules[d] = append(g.rules[d], mapRule{
			min: n("minEnemies"), threshold: n("threshold"),
			bots: strings.Fields(e.sub.value("bots")), tasks: tasks,
		})
	}
	if g.maps == nil {
		g.maps = maps
	} else if strings.Join(maps, " ") != strings.Join(g.maps, " ") {
		log.Fatalf("%s: the map list differs between difficulties", difficulties[d])
	}
}

func (g *gameData) goSource() ([]byte, error) {
	var b bytes.Buffer
	p := func(f string, a ...any) { fmt.Fprintf(&b, f, a...) }
	p("// Code generated by tools/extract from the game's CareerGame*.vdf and\n")
	p("// BotCampaignProfile.db. DO NOT EDIT.\n\npackage game\n\n")
	p("var mapIDs = %#v\n\n", g.maps)
	p("var startPoints = %#v\n\n", g.startPoints)
	p("var matchWins = %#v\n\n", g.matchWins)
	p("var matchWinBy = %#v\n\n", g.matchWinBy)
	p("// costAvailability[difficulty][cost]: reputation points needed to hire a\n")
	p("// teammate of that cost (0 = not set: that cost and above are always available).\n")
	p("var costAvailability = %#v\n\n", g.costAvail)
	p("var mapRules = [4][]MapRule{\n")
	for d := range difficulties {
		p("\t{ // %s\n", difficulties[d])
		for _, r := range g.rules[d] {
			p("\t\t{MinEnemies: %d, Threshold: %d, Bots: %#v, Tasks: %#v},\n", r.min, r.threshold, r.bots, r.tasks)
		}
		p("\t},\n")
	}
	p("}\n\n// teammates in the order of the \"Characters\" line of CareerGame*.vdf,\n")
	p("// with their values from BotCampaignProfile.db after template inheritance.\n")
	p("var teammates = []Teammate{\n")
	for _, t := range g.teammates {
		p("\t{Name: %q, Cost: %d, Skin: %d, Skill: %g, Aggression: %g, Teamwork: %g, ReactionTime: %g, AttackDelay: %g, VoicePitch: %d, Difficulty: %#v, Weapons: %#v},\n",
			t.name, t.cost, t.skin, t.skill, t.aggression, t.teamwork, t.reactionTime, t.attackDelay, t.voicePitch, t.difficulty, t.weapons)
	}
	p("}\n\n// terrorists: every other profile of BotCampaignProfile.db, in file order.\n")
	p("// Level is the first template of the profile (its skill bracket).\n")
	p("var terrorists = []Terrorist{\n")
	for _, t := range g.terrorists {
		p("\t{Name: %q, Level: %q, Skin: %d, Skill: %g, Aggression: %g, Teamwork: %g, ReactionTime: %g, AttackDelay: %g, Weapons: %#v},\n",
			t.name, t.level, t.skin, t.skill, t.aggression, t.teamwork, t.reactionTime, t.attackDelay, t.weapons)
	}
	p("}\n")
	return format.Source(b.Bytes())
}

// ---------------------------------------------------------------------------
// Valve KeyValues (.vdf)
// ---------------------------------------------------------------------------

type kvItem struct {
	key, val string
	sub      *kv
}

type kv struct{ items []kvItem }

func (k *kv) child(key string) *kv {
	if k == nil {
		return nil
	}
	for _, it := range k.items {
		if strings.EqualFold(it.key, key) && it.sub != nil {
			return it.sub
		}
	}
	return nil
}

func (k *kv) value(key string) string {
	if k == nil {
		return ""
	}
	for _, it := range k.items {
		if strings.EqualFold(it.key, key) && it.sub == nil {
			return it.val
		}
	}
	return ""
}

func parseKV(path string) (*kv, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	toks := tokenize(string(data))
	pos := 0
	var parse func() *kv
	parse = func() *kv {
		k := &kv{}
		for pos < len(toks) {
			t := toks[pos]
			pos++
			if t == "}" {
				return k
			}
			if pos < len(toks) && toks[pos] == "{" {
				pos++
				k.items = append(k.items, kvItem{key: t, sub: parse()})
			} else if pos < len(toks) {
				k.items = append(k.items, kvItem{key: t, val: toks[pos]})
				pos++
			}
		}
		return k
	}
	return parse(), nil
}

func tokenize(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '{' || c == '}':
			toks = append(toks, string(c))
			i++
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				j++
			}
			toks = append(toks, s[i+1:j])
			i = j + 1
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\r\n{}\"", rune(s[j])) {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks
}

// ---------------------------------------------------------------------------
// BotCampaignProfile.db
//
// Same rules as the game's loader (ReGameDLL game_shared/bot/bot_profile.cpp):
//   - attribute names are case-insensitive ("SKin = 1" for Sam counts);
//   - "Default" sets the baseline, "Template X" blocks start from it;
//   - a profile "A+B Name" starts from Default, then inherits each template in
//     order, taking every value that differs from the baseline (Inherit);
//   - inside a block, the first WeaponPreference line replaces the inherited
//     list, and "none" empties it.
// ---------------------------------------------------------------------------

type rawBlock struct {
	kind, name string
	templates  []string
	attrs      [][2]string // lower-case name, value
}

func parseBotProfiles(path string) ([]botProfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var blocks []rawBlock
	var cur *rawBlock
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if cur == nil {
			fields := strings.Fields(line)
			switch {
			case strings.EqualFold(fields[0], "Default"):
				cur = &rawBlock{kind: "default"}
			case strings.EqualFold(fields[0], "Template") && len(fields) == 2:
				cur = &rawBlock{kind: "template", name: fields[1]}
			case len(fields) == 2:
				cur = &rawBlock{kind: "profile", name: fields[1], templates: strings.Split(fields[0], "+")}
			}
			continue
		}
		if strings.EqualFold(line, "End") {
			blocks = append(blocks, *cur)
			cur = nil
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur.attrs = append(cur.attrs, [2]string{strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	apply := func(p *botProfile, attrs [][2]string) {
		firstWeapon := true
		for _, a := range attrs {
			k, v := a[0], a[1]
			num, _ := strconv.ParseFloat(v, 64)
			switch k {
			case "skill":
				p.skill = num
			case "aggression":
				p.aggression = num
			case "teamwork":
				p.teamwork = num
			case "reactiontime":
				p.reactionTime = num
			case "attackdelay":
				p.attackDelay = num
			case "cost":
				p.cost = int(num)
			case "skin":
				p.skin = int(num)
			case "voicepitch":
				p.voicePitch = int(num)
			case "difficulty":
				p.difficulty = strings.Split(strings.ToUpper(v), "+")
			case "weaponpreference":
				if firstWeapon {
					firstWeapon = false
					p.weapons = nil
				}
				if !strings.EqualFold(v, "none") {
					p.weapons = append(p.weapons, strings.ToLower(v))
				}
			}
		}
	}
	inherit := func(p *botProfile, parent, base botProfile) {
		if parent.skill != base.skill {
			p.skill = parent.skill
		}
		if parent.aggression != base.aggression {
			p.aggression = parent.aggression
		}
		if parent.teamwork != base.teamwork {
			p.teamwork = parent.teamwork
		}
		if len(parent.weapons) != len(base.weapons) {
			p.weapons = append([]string(nil), parent.weapons...)
		}
		if parent.cost != base.cost {
			p.cost = parent.cost
		}
		if parent.skin != base.skin {
			p.skin = parent.skin
		}
		if strings.Join(parent.difficulty, "+") != strings.Join(base.difficulty, "+") {
			p.difficulty = parent.difficulty
		}
		if parent.voicePitch != base.voicePitch {
			p.voicePitch = parent.voicePitch
		}
		if parent.reactionTime != base.reactionTime {
			p.reactionTime = parent.reactionTime
		}
		if parent.attackDelay != base.attackDelay {
			p.attackDelay = parent.attackDelay
		}
	}

	var base botProfile
	templates := map[string]botProfile{}
	var out []botProfile
	for _, b := range blocks {
		switch b.kind {
		case "default":
			apply(&base, b.attrs)
		case "template":
			t := base
			t.weapons = append([]string(nil), base.weapons...)
			apply(&t, b.attrs)
			templates[strings.ToLower(b.name)] = t
		case "profile":
			p := base
			p.weapons = append([]string(nil), base.weapons...)
			for _, name := range b.templates {
				t, ok := templates[strings.ToLower(name)]
				if !ok {
					return nil, fmt.Errorf("%s: unknown template %s", b.name, name)
				}
				inherit(&p, t, base)
			}
			apply(&p, b.attrs)
			p.name, p.level = b.name, b.templates[0]
			out = append(out, p)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// TGA (uncompressed and RLE, 24/32-bit) to PNG
// ---------------------------------------------------------------------------

func tgaToPNG(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	img, err := decodeTGA(data)
	if err != nil {
		return fmt.Errorf("%s: %w", from, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(to, buf.Bytes(), 0o644)
}

func decodeTGA(d []byte) (image.Image, error) {
	if len(d) < 18 {
		return nil, fmt.Errorf("not a TGA file")
	}
	idLen, cmapType, typ := int(d[0]), d[1], d[2]
	w, h := int(d[12])|int(d[13])<<8, int(d[14])|int(d[15])<<8
	bpp, desc := int(d[16]), d[17]
	if cmapType != 0 || (typ != 2 && typ != 10) || (bpp != 24 && bpp != 32) {
		return nil, fmt.Errorf("unsupported TGA (type %d, %d bpp)", typ, bpp)
	}
	px := bpp / 8
	src := d[18+idLen:]
	raw := make([]byte, 0, w*h*px)
	if typ == 2 {
		if len(src) < w*h*px {
			return nil, fmt.Errorf("truncated TGA")
		}
		raw = src[:w*h*px]
	} else {
		for i := 0; len(raw) < w*h*px; {
			if i >= len(src) {
				return nil, fmt.Errorf("truncated RLE TGA")
			}
			hdr := int(src[i])
			i++
			n := hdr&0x7f + 1
			if hdr&0x80 != 0 {
				for k := 0; k < n; k++ {
					raw = append(raw, src[i:i+px]...)
				}
				i += px
			} else {
				raw = append(raw, src[i:i+n*px]...)
				i += n * px
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	topDown := desc&0x20 != 0
	for y := 0; y < h; y++ {
		row := y
		if !topDown {
			row = h - 1 - y
		}
		for x := 0; x < w; x++ {
			o := (y*w + x) * px
			a := uint8(255)
			if px == 4 {
				a = raw[o+3]
			}
			img.SetNRGBA(x, row, color.NRGBA{raw[o+2], raw[o+1], raw[o], a})
		}
	}
	return img, nil
}
