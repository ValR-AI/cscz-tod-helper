package bundle

import (
	"testing"

	"todhelper/internal/savestate"
)

func TestBundleLifecycle(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	b, err := st.Create("Easy ILs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Create("Easy ILs"); err == nil {
		t.Fatal("duplicate bundle accepted")
	}
	if err := b.Put(savestate.Savestate{Name: "Repo_Italy"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(savestate.Savestate{Name: "repo_italy"}, ""); err == nil {
		t.Fatal("duplicate savestate name accepted")
	}
	if err := b.Put(savestate.Savestate{Name: "Repo_Italy2"}, "Repo_Italy"); err != nil || b.Savestates[0].Name != "Repo_Italy2" {
		t.Fatalf("edit by original name: %v", err)
	}
	if err := st.Save(b); err != nil {
		t.Fatal(err)
	}
	other, _ := st.Create("Other")
	other.Put(savestate.Savestate{Name: "Repo_Italy2"}, "")
	st.Save(other)
	if users := st.OtherBundlesUsing("REPO_ITALY2", "Easy ILs"); len(users) != 1 || users[0] != "Other" {
		t.Fatalf("other bundles using: %v", users)
	}
	if err := st.Rename("Easy ILs", "easy ils"); err != nil {
		t.Fatal(err)
	}
	names, _ := st.List()
	if len(names) != 2 || names[0] != "easy ils" {
		t.Fatalf("after case rename: %v", names)
	}
	loaded, err := st.Load("easy ils")
	if err != nil || len(loaded.Savestates) != 1 {
		t.Fatalf("load: %v", err)
	}
	if err := st.Delete("easy ils"); err != nil {
		t.Fatal(err)
	}
	if names, _ := st.List(); len(names) != 1 {
		t.Fatalf("after delete: %v", names)
	}
	for _, bad := range []string{"", " x", "../x", "a/b", ".hidden"} {
		if CheckName(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
