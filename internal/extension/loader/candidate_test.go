package loader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectMode_SingleTS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.ts")
	if err := os.WriteFile(path, []byte("export default () => {}"), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := detectMode(path)
	if err != nil {
		t.Fatal(err)
	}
	if m != ModeHostedTS {
		t.Fatalf("expected hosted-ts; got %v", m)
	}
}

func TestDetectMode_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"x","pi":{"entry":"./src/index.ts"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := detectMode(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m != ModeHostedTS {
		t.Fatalf("expected hosted-ts from package.json; got %v", m)
	}
}

func TestDetectMode_PiToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"),
		[]byte(`name="x"`+"\n"+`version="0.1"`+"\n"+`runtime="hosted"`), 0644); err != nil {
		t.Fatal(err)
	}
	m, err := detectMode(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m != ModeHostedGo {
		t.Fatalf("expected hosted-go; got %v", m)
	}
}
