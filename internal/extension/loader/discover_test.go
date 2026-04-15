package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_LayeredOverrides(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	writeExt(t, filepath.Join(home, ".pi-go", "extensions", "ext-a"), `name="ext-a"`+"\n"+`version="0.1"`+"\n"+`runtime="hosted"`+"\n"+`command=["go", "run", "."]`)
	writeExt(t, filepath.Join(proj, ".pi-go", "extensions", "ext-a"), `name="ext-a"`+"\n"+`version="0.2"`+"\n"+`runtime="hosted"`+"\n"+`command=["go", "run", "."]`)

	got, err := Discover(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate; got %d", len(got))
	}
	if got[0].Metadata.Version != "0.2" {
		t.Fatalf("expected project override v0.2; got %s", got[0].Metadata.Version)
	}
}

func TestDiscover_HomeOnly(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	writeExt(t, filepath.Join(home, ".pi-go", "extensions", "alpha"), `name="alpha"`+"\n"+`version="1.0"`+"\n"+`runtime="hosted"`+"\n"+`command=["go", "run", "."]`)

	got, err := Discover(proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Metadata.Name != "alpha" {
		t.Fatalf("expected alpha from home; got %+v", got)
	}
}

func writeExt(t *testing.T, dir, piToml string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(piToml), 0644); err != nil {
		t.Fatal(err)
	}
}
