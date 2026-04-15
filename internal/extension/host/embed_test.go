package host

import (
	"os"
	"runtime"
	"testing"
)

func TestExtractedHostPath(t *testing.T) {
	tmp := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}
	path, err := ExtractedHostPath("test-" + t.Name())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat extracted: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("extracted file is empty")
	}
}

func TestEmbeddedBundleNonEmpty(t *testing.T) {
	if len(embeddedHost) == 0 {
		t.Fatal("embedded host bundle is empty — did you run `npm run bundle` first?")
	}
}
