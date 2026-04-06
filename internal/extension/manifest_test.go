package extension

import (
	"path/filepath"
	"testing"
)

func TestLoadManifests_ParsesHostedRuntimeBlock(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "hosted"), `{
		"name": "hosted",
		"runtime": {
			"type": "hosted_stdio_jsonrpc",
			"command": "demo-host",
			"args": ["serve", "--stdio"],
			"env": {"LOG_LEVEL":"debug"}
		}
	}`)

	manifests, err := LoadManifests(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	if manifests[0].Runtime.Type != RuntimeTypeHostedStdioJSONRPC {
		t.Fatalf("expected runtime type %q, got %q", RuntimeTypeHostedStdioJSONRPC, manifests[0].Runtime.Type)
	}
	if manifests[0].Runtime.Command != "demo-host" {
		t.Fatalf("expected runtime command demo-host, got %q", manifests[0].Runtime.Command)
	}
	if len(manifests[0].Runtime.Args) != 2 {
		t.Fatalf("expected runtime args to parse, got %+v", manifests[0].Runtime.Args)
	}
	if manifests[0].Runtime.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("expected runtime env LOG_LEVEL=debug, got %+v", manifests[0].Runtime.Env)
	}
}

func TestLoadManifests_BackwardCompatibleWithoutRuntimeBlock(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "legacy"), `{
		"name": "legacy",
		"prompt": "legacy prompt",
		"tui": {"commands": [{"name":"legacy"}]}
	}`)

	manifests, err := LoadManifests(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	if manifests[0].Runtime.Type != RuntimeTypeDeclarative {
		t.Fatalf("expected declarative runtime for old manifest, got %q", manifests[0].Runtime.Type)
	}
	if manifests[0].Runtime.Command != "" {
		t.Fatalf("expected empty runtime command, got %q", manifests[0].Runtime.Command)
	}
}
