package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"todhelper/internal/savestate"
)

type client struct {
	t *testing.T
	h http.Handler
}

func (c client) do(method, path string, body any, headers bool) (int, map[string]any) {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Host = "127.0.0.1:47634"
	if headers {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ToD-Helper", "1")
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (c client) post(path string, body any) (int, map[string]any) {
	return c.do("POST", path, body, true)
}

func (c client) mustPost(path string, body any) map[string]any {
	code, out := c.post(path, body)
	if code != 200 {
		c.t.Fatalf("%s: %d %v", path, code, out)
	}
	return out
}

func savestates(state map[string]any) []any {
	if st, ok := state["state"].(map[string]any); ok {
		state = st
	}
	list, _ := state["savestates"].([]any)
	return list
}

func statusOf(state map[string]any, name string) string {
	for _, v := range savestates(state) {
		m := v.(map[string]any)
		if m["savestate"].(map[string]any)["name"] == name {
			return m["status"].(map[string]any)["state"].(string)
		}
	}
	return "absent"
}

func TestAPI(t *testing.T) {
	data := t.TempDir()
	save := t.TempDir()
	app, err := NewApp("test", data, 47634)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	c := client{t, app.Handler(47634)}

	// Security: wrong host, missing header.
	req := httptest.NewRequest("GET", "/api/state", nil)
	req.Host = "evil.example:47634"
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("foreign host allowed: %d", rec.Code)
	}
	if code, _ := c.do("POST", "/api/bundle/reset", map[string]any{}, false); code != 403 {
		t.Fatalf("POST without the helper header allowed: %d", code)
	}

	// First start: a default bundle, no Save folder yet.
	code, st := c.do("GET", "/api/state", nil, false)
	if code != 200 || st["active"] != "My bundle" || st["saveDirError"] == "" {
		t.Fatalf("initial state: %v", st)
	}
	if code, _ := c.post("/api/settings", map[string]any{"saveDir": filepath.Join(save, "nope")}); code != 400 {
		t.Fatal("a missing Save folder must be refused")
	}
	c.mustPost("/api/settings", map[string]any{"saveDir": save})

	// Evaluate normalizes.
	ev := c.mustPost("/api/evaluate", map[string]any{"name": "x", "beaten": []string{"de_tides_cz"}})
	if ev["beatenCount"].(float64) != 0 {
		t.Fatalf("tides alone must be dropped: %v", ev["savestate"])
	}

	// Save a savestate: the profile appears, read-only.
	italy := map[string]any{"name": "Repo_Italy", "difficulty": "easy",
		"beaten":  []string{"de_dust_cz", "cs_office_cz", "de_prodigy_cz"},
		"current": "cs_italy_cz", "team": []string{"floyd", "morris", "quincy", "cooper", "vern"}, "readOnly": true}
	out := c.mustPost("/api/savestate/save", map[string]any{"savestate": italy})
	path := savestate.Path(save, "Repo_Italy")
	if !savestate.IsReadOnly(path) || statusOf(out, "Repo_Italy") != "ready" {
		t.Fatalf("after save: %v", statusOf(out, "Repo_Italy"))
	}

	// A profile the bundle does not know needs confirmation, and is backed up.
	os.WriteFile(savestate.Path(save, "Mine"), []byte("my real career"), 0o644)
	if code, out := c.post("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "Mine"}}); code != 409 || out["needsOverwrite"] != true {
		t.Fatalf("overwrite must be confirmed: %d %v", code, out)
	}
	c.mustPost("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "Mine"}, "overwrite": true})
	backups, _ := filepath.Glob(filepath.Join(data, "backups", "CZProfile-Mine.czp.*.bak"))
	if len(backups) != 1 {
		t.Fatal("the overwritten profile was not backed up")
	}
	if entries, _ := os.ReadDir(save); len(entries) != 2 {
		t.Fatalf("only the two profiles may exist in Save, found %d entries", len(entries))
	}

	// The game wins a map in a writable copy; reset brings it back.
	won, _ := savestate.Bytes(savestate.Savestate{Name: "Repo_Italy", Beaten: []string{"de_dust_cz", "cs_office_cz", "de_prodigy_cz", "cs_italy_cz"}})
	savestate.WriteFile(path, won, false)
	_, st = c.do("GET", "/api/state", nil, false)
	if statusOf(st, "Repo_Italy") != "changed" {
		t.Fatal("expected changed")
	}
	out = c.mustPost("/api/bundle/reset", map[string]any{})
	if out["restored"].(float64) != 2 || statusOf(out, "Repo_Italy") != "ready" || !savestate.IsReadOnly(path) {
		t.Fatalf("reset: %v", out["restored"])
	}

	// Renaming moves the profile file.
	italy["name"] = "Repo_Italy_B"
	c.mustPost("/api/savestate/save", map[string]any{"savestate": italy, "original": "Repo_Italy"})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the old profile should be removed after a rename")
	}
	if _, err := os.Stat(savestate.Path(save, "Repo_Italy_B")); err != nil {
		t.Fatal("renamed profile missing")
	}
	if code, _ := c.post("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "mine"}}); code != 409 {
		t.Fatal("duplicate name in the bundle accepted")
	}

	// Bundles: a second one, a name clash warning, deleting removes its profiles.
	c.mustPost("/api/bundles", map[string]any{"action": "create", "name": "Expert"})
	out = c.mustPost("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "Repo_Italy_B", "difficulty": "expert"}})
	if w, _ := out["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "My bundle") {
		t.Fatalf("expected a shared-name warning: %v", out["warnings"])
	}
	c.mustPost("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "Only_Expert", "difficulty": "expert"}})
	c.mustPost("/api/bundles", map[string]any{"action": "rename", "name": "Expert", "newName": "Expert ILs"})
	out = c.mustPost("/api/bundles", map[string]any{"action": "delete", "name": "Expert ILs"})
	if _, err := os.Stat(savestate.Path(save, "Only_Expert")); !os.IsNotExist(err) {
		t.Fatal("deleting a bundle should delete its profiles")
	}
	if out["active"] != "My bundle" {
		t.Fatalf("active bundle after delete: %v", out["active"])
	}

	// Delete a savestate.
	out = c.mustPost("/api/savestate/delete", map[string]any{"name": "Mine"})
	if _, err := os.Stat(savestate.Path(save, "Mine")); !os.IsNotExist(err) || len(savestates(out)) != 1 {
		t.Fatal("delete failed")
	}

	// Autosplit settings.
	c.mustPost("/api/settings", map[string]any{"liveSplitAddr": "127.0.0.1:1"})
	if code, _ := c.post("/api/settings", map[string]any{"liveSplitAddr": "nonsense"}); code != 400 {
		t.Fatal("bad LiveSplit address accepted")
	}
	as := c.mustPost("/api/autosplit", map[string]any{"enabled": true, "file": "CZProfile-Repo_Italy_B.czp"})
	if as["enabled"] != true || as["profile"] != "Repo_Italy_B" || as["file"] != "CZProfile-Repo_Italy_B.czp" {
		t.Fatalf("autosplit: %v", as)
	}
	_, profiles := c.do("GET", "/api/profiles", nil, false)
	_ = profiles
	c.mustPost("/api/autosplit", map[string]any{"enabled": false})

	// A name with parentheses is written the way the game names the file, and a
	// name that gives the same file is refused.
	c.mustPost("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "ValR_ToD1(U)"}})
	if _, err := os.Stat(filepath.Join(save, "CZProfile-ValR_ToD1_U_.czp")); err != nil {
		t.Fatal("ValR_ToD1(U) must be written as CZProfile-ValR_ToD1_U_.czp")
	}
	if code, out := c.post("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "ValR_ToD1_U_"}}); code != 409 || !strings.Contains(out["error"].(string), "same file") {
		t.Fatalf("same file accepted: %d %v", code, out)
	}
	// Renaming to a name with the same file keeps the file.
	c.mustPost("/api/savestate/save", map[string]any{"savestate": map[string]any{"name": "ValR_ToD1[U]"}, "original": "ValR_ToD1(U)"})
	if _, err := os.Stat(filepath.Join(save, "CZProfile-ValR_ToD1_U_.czp")); err != nil {
		t.Fatal("a rename to a same-file name must not delete the file")
	}

	// Import: a profile made elsewhere joins the bundle, the file is untouched.
	foreign, _ := savestate.Bytes(savestate.Savestate{Name: "Friend(2)", Difficulty: "normal", Beaten: []string{"de_dust_cz"}, Current: "cs_office_cz"})
	os.WriteFile(filepath.Join(save, "CZProfile-Friend_2_.czp"), foreign, 0o644)
	os.WriteFile(filepath.Join(save, "CZProfile-Broken.czp"), []byte("nope"), 0o644)
	req = httptest.NewRequest("GET", "/api/import", nil)
	req.Host = "127.0.0.1:47634"
	rec = httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	var cands []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cands)
	found := map[string]map[string]any{}
	for _, x := range cands {
		found[x["file"].(string)] = x
	}
	if found["CZProfile-Friend_2_.czp"]["importable"] != true || found["CZProfile-Friend_2_.czp"]["name"] != "Friend(2)" ||
		found["CZProfile-Broken.czp"]["importable"] != false || found["CZProfile-ValR_ToD1_U_.czp"]["reason"] != "Already in this bundle" || found["CZProfile-ValR_ToD1_U_.czp"]["name"] != "ValR_ToD1[U]" {
		t.Fatalf("import candidates: %v", cands)
	}
	out = c.mustPost("/api/import", map[string]any{"files": []string{"CZProfile-Friend_2_.czp"}})
	if out["imported"].(float64) != 1 || statusOf(out, "Friend(2)") != "ready" {
		t.Fatalf("import: %v", out["results"])
	}
	os.Remove(filepath.Join(save, "CZProfile-Broken.czp"))

	// ToD pack: a new bundle with 12 written profiles.
	out = c.mustPost("/api/pack", map[string]any{"pseudo": "ValR", "difficulty": "easy"})
	if out["bundle"] != "ValR ToD Easy" || len(savestates(out)) != 12 || statusOf(out, "ValR_ToD6-L") != "ready" {
		t.Fatalf("pack: %v", out["bundle"])
	}
	if !savestate.IsReadOnly(savestate.Path(save, "ValR_ToD3-L")) || savestate.IsReadOnly(savestate.Path(save, "ValR_ToD3-U")) {
		t.Fatal("L must be read-only, U writable")
	}
	out = c.mustPost("/api/pack", map[string]any{"pseudo": "ValR", "difficulty": "easy"})
	if out["bundle"] != "ValR ToD Easy (2)" || len(out["warnings"].([]any)) != 1 {
		t.Fatalf("second pack: %v %v", out["bundle"], out["warnings"])
	}
	os.WriteFile(savestate.Path(save, "Mine_ToD1-U"), []byte("my real career"), 0o644)
	if code, out := c.post("/api/pack", map[string]any{"pseudo": "Mine", "difficulty": "hard"}); code != 409 || out["needsOverwrite"] != true {
		t.Fatalf("pack over a foreign profile: %d %v", code, out)
	}
	c.mustPost("/api/pack", map[string]any{"pseudo": "Mine", "difficulty": "hard", "overwrite": true})
	c.mustPost("/api/bundles", map[string]any{"action": "select", "name": "My bundle"})

	// Terrorist sheets.
	req = httptest.NewRequest("GET", "/api/terrorists", nil)
	req.Host = "127.0.0.1:47634"
	rec = httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	var foes []map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &foes); rec.Code != 200 || len(foes) != 32 {
		t.Fatalf("terrorists: %d, %d", rec.Code, len(foes))
	}

	// Every write and delete is in the helper's log, and no temporary file is left.
	logs, _ := filepath.Glob(filepath.Join(data, "logs", "helper-*.log"))
	if len(logs) != 1 {
		t.Fatal("no helper log")
	}
	text, _ := os.ReadFile(logs[0])
	for _, want := range []string{"START", "WRITE    CZProfile-Repo_Italy.czp (Easy, 3/18 won", "BACKUP", "RESET", "DELETE   CZProfile-Repo_Italy.czp [old name of Repo_Italy_B]", "DELETE   CZProfile-Only_Expert.czp [bundle Expert ILs deleted]", "WATCH", "IMPORT   CZProfile-Friend_2_.czp", "PACK     recommended ToD pack for ValR on Easy"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("log misses %q:\n%s", want, text)
		}
	}
	if tmp, _ := filepath.Glob(filepath.Join(save, ".cscz-tod-helper-*")); len(tmp) != 0 {
		t.Errorf("temporary files left: %v", tmp)
	}

	// Settings survive a restart.
	app.Close()
	app2, err := NewApp("test", data, 47634)
	if err != nil {
		t.Fatal(err)
	}
	defer app2.Close()
	if app2.settings.SaveDir != save || app2.settings.LiveSplitAddr != "127.0.0.1:1" {
		t.Fatalf("settings not kept: %+v", app2.settings)
	}
}

func TestUIIsServed(t *testing.T) {
	app, err := NewApp("test", t.TempDir(), 47634)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, p := range []string{"/", "/app.js", "/style.css", "/img/maps/de_dust_cz.png", "/img/models/3.png", "/img/tiers/5.png"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Host = "localhost:47634"
		rec := httptest.NewRecorder()
		app.Handler(47634).ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}
