package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidateDistRejectsMissingReferencedAsset(t *testing.T) {
	t.Parallel()

	err := ValidateDist(fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><html><head><script type="module" src="/assets/index.js"></script></head><body><div id="root"></div></body></html>`),
		},
	})
	if err == nil {
		t.Fatal("expected error for missing referenced asset")
	}
	if !strings.Contains(err.Error(), "assets/index.js") {
		t.Fatalf("error = %q, want missing asset path", err)
	}
}

func TestPrepareDevelopmentDistAtRebuildsInvalidBundle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := sourcePaths{
		distDir:  filepath.Join(root, "dist"),
		panelDir: filepath.Join(root, "panel"),
	}

	if err := os.MkdirAll(filepath.Join(paths.distDir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir dist assets: %v", err)
	}
	if err := os.MkdirAll(paths.panelDir, 0o755); err != nil {
		t.Fatalf("mkdir panel dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.panelDir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.distDir, "index.html"), []byte(`<!doctype html><html><head><script type="module" src="/assets/index-old.js"></script></head><body><div id="root"></div></body></html>`), 0o644); err != nil {
		t.Fatalf("write stale index.html: %v", err)
	}

	called := 0
	previousBuild := runPanelBuild
	runPanelBuild = func(ctx context.Context, panelDir string) error {
		called++

		if panelDir != paths.panelDir {
			t.Fatalf("panelDir = %q, want %q", panelDir, paths.panelDir)
		}

		if err := os.WriteFile(filepath.Join(paths.distDir, "index.html"), []byte(`<!doctype html><html><head><script type="module" src="/assets/index-new.js"></script></head><body><div id="root"></div></body></html>`), 0o644); err != nil {
			t.Fatalf("rewrite index.html: %v", err)
		}
		if err := os.WriteFile(filepath.Join(paths.distDir, "assets", "index-new.js"), []byte("console.log('ok')"), 0o644); err != nil {
			t.Fatalf("write rebuilt asset: %v", err)
		}

		return nil
	}
	t.Cleanup(func() {
		runPanelBuild = previousBuild
	})

	if err := prepareDevelopmentDistAt(context.Background(), paths); err != nil {
		t.Fatalf("prepareDevelopmentDistAt: %v", err)
	}
	if called != 1 {
		t.Fatalf("runPanelBuild calls = %d, want 1", called)
	}

	distFS, err := dirFS(paths.distDir)
	if err != nil {
		t.Fatalf("dirFS: %v", err)
	}
	if err := ValidateDist(distFS); err != nil {
		t.Fatalf("ValidateDist after rebuild: %v", err)
	}
}
