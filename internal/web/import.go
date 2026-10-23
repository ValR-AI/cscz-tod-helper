package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"todhelper/internal/bundle"
	"todhelper/internal/czp"
	"todhelper/internal/savestate"
)

// importCandidate is a profile of the Save folder, with whether it can
// become a savestate of the active bundle.
type importCandidate struct {
	savestate.ProfileFile
	Importable bool   `json:"importable"`
	Reason     string `json:"reason"` // why it cannot be imported
}

// whyNotImportable checks a profile of the Save folder against the bundle.
func whyNotImportable(b *bundle.Bundle, pf savestate.ProfileFile) string {
	switch {
	case !pf.Valid:
		return "Unreadable or damaged profile"
	case b.Find(pf.Name) >= 0 && strings.EqualFold(savestate.FileName(pf.Name), pf.File):
		return "Already in this bundle"
	case len(pf.Name) > savestate.MaxNameLength:
		return fmt.Sprintf("Its name is longer than %d characters", savestate.MaxNameLength)
	case savestate.CheckName(pf.Name) != nil:
		return "Its name has characters the helper does not use: " + savestate.CheckName(pf.Name).Error()
	case !strings.EqualFold(savestate.FileName(pf.Name), pf.File):
		return fmt.Sprintf("Its file should be called %s for the game to load it", savestate.FileName(pf.Name))
	case b.Find(pf.Name) >= 0:
		return "This bundle already has a savestate using the same file"
	}
	return ""
}

func (a *App) importCandidates(r *http.Request) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	list, err := savestate.ListProfiles(a.settings.SaveDir)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	b, err := a.store.Load(a.settings.ActiveBundle)
	if err != nil {
		return nil, err
	}
	out := []importCandidate{}
	for _, pf := range list {
		reason := whyNotImportable(b, pf)
		out = append(out, importCandidate{ProfileFile: pf, Importable: reason == "", Reason: reason})
	}
	return out, nil
}

// importProfiles adds profiles of the Save folder to the active bundle. The
// files themselves are left as they are.
func (a *App) importProfiles(r *http.Request) (any, error) {
	var req struct {
		Files []string `json:"files"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dir := a.settings.SaveDir
	if err := savestate.CheckSaveDir(dir); err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	b, err := a.store.Load(a.settings.ActiveBundle)
	if err != nil {
		return nil, err
	}
	type result struct {
		File    string   `json:"file"`
		Name    string   `json:"name"`
		Error   string   `json:"error,omitempty"`
		Notices []string `json:"notices"`
	}
	var results []result
	imported := 0
	for _, file := range req.Files {
		res := result{File: file, Notices: []string{}}
		if !savestate.IsProfileFile(file) {
			res.Error = "not a profile file"
			results = append(results, res)
			continue
		}
		pf := savestate.ReadProfileFile(dir, file)
		res.Name = pf.Name
		if why := whyNotImportable(b, pf); why != "" {
			res.Error = why
			results = append(results, res)
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, file))
		var p *czp.Profile
		if err == nil {
			p, err = czp.Parse(data)
		}
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		s, notices := savestate.FromProfile(p, pf.ReadOnly)
		s = savestate.Evaluate(s).Savestate
		if err := b.Put(s, ""); err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.Notices = append(res.Notices, notices...)
		imported++
		a.log.add("IMPORT", "%s (%s) into bundle %s; the file is left as it is", file, savestate.Describe(s), b.Name)
		results = append(results, res)
	}
	if imported > 0 {
		if err := a.store.Save(b); err != nil {
			return nil, err
		}
	}
	st, err := a.stateLocked()
	return map[string]any{"state": st, "imported": imported, "results": results}, err
}

// makePack creates a new bundle holding the recommended ToD pack and writes
// its twelve profiles.
func (a *App) makePack(r *http.Request) (any, error) {
	var req struct {
		Pseudo     string `json:"pseudo"`
		Difficulty string `json:"difficulty"`
		Overwrite  bool   `json:"overwrite"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	pack, err := savestate.ToDPack(req.Pseudo, req.Difficulty)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dir := a.settings.SaveDir
	if err := savestate.CheckSaveDir(dir); err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	// Profiles no bundle knows (a real career, for example) need a yes first.
	var foreign []string
	for _, s := range pack {
		if _, err := os.Stat(savestate.Path(dir, s.Name)); err == nil && len(a.store.OtherBundlesUsing(s.Name, "")) == 0 {
			foreign = append(foreign, s.Name)
		}
	}
	if len(foreign) > 0 && !req.Overwrite {
		return nil, &apiError{status: http.StatusConflict,
			msg: fmt.Sprintf("Your Save folder already has %s. Overwrite? A copy of each is kept in the helper's backups folder.",
				strings.Join(foreign, ", ")),
			extra: map[string]any{"needsOverwrite": true}}
	}
	var warnings []string
	others := map[string]bool{}
	for _, s := range pack {
		for _, o := range a.store.OtherBundlesUsing(s.Name, "") {
			others[o] = true
		}
	}
	if len(others) > 0 {
		var names []string
		for o := range others {
			names = append(names, o)
		}
		warnings = append(warnings, fmt.Sprintf("The bundle(s) %s already use some of these names: they write the same files.", strings.Join(names, ", ")))
	}

	diff := strings.ToUpper(pack[0].Difficulty[:1]) + pack[0].Difficulty[1:]
	base := bundleSafe(strings.TrimSpace(req.Pseudo)) + " ToD " + diff
	name := base
	for i := 2; a.store.Exists(name); i++ {
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	b, err := a.store.Create(name)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	for _, f := range foreign {
		if dest, err := a.backup(savestate.Path(dir, f)); err == nil {
			a.log.add("BACKUP", "%s copied to %s before overwriting it", savestate.FileName(f), dest)
		}
	}
	a.log.add("PACK", "recommended ToD pack for %s on %s: bundle %s", strings.TrimSpace(req.Pseudo), diff, name)
	for _, s := range pack {
		s = savestate.Evaluate(s).Savestate
		if err := b.Put(s, ""); err != nil {
			return nil, err
		}
		if err := a.install(dir, s, "ToD pack, bundle "+name); err != nil {
			return nil, err
		}
	}
	if err := a.store.Save(b); err != nil {
		return nil, err
	}
	a.settings.ActiveBundle = name
	if err := a.saveSettingsLocked(); err != nil {
		return nil, err
	}
	st, err := a.stateLocked()
	return map[string]any{"state": st, "bundle": name, "warnings": warnings}, err
}

// bundleSafe keeps the characters a bundle name accepts.
func bundleSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(" _-.()", r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return strings.TrimLeft(b.String(), ".")
}
