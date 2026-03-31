package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

//go:embed all:dist
var dist embed.FS

var panelAssetPattern = regexp.MustCompile(`(?:src|href)=["']([^"']+)["']`)

var runPanelBuild = func(ctx context.Context, panelDir string) error {
	cmd := exec.CommandContext(ctx, "npm", "run", "build")
	cmd.Dir = panelDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type sourcePaths struct {
	distDir  string
	panelDir string
}

func DistFS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

func DevelopmentDistFS() (fs.FS, error) {
	paths, ok := localSourcePaths()
	if !ok {
		return DistFS()
	}

	distFS, err := dirFS(paths.distDir)
	if err != nil {
		return DistFS()
	}

	if err := ValidateDist(distFS); err != nil {
		return DistFS()
	}

	return distFS, nil
}

func PrepareDevelopmentDist(ctx context.Context) error {
	paths, ok := localSourcePaths()
	if !ok {
		return nil
	}

	return prepareDevelopmentDistAt(ctx, paths)
}

func ValidateDist(distFS fs.FS) error {
	index, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return err
	}

	matches := panelAssetPattern.FindAllSubmatch(index, -1)
	for _, match := range matches {
		assetPath, ok := normalizeAssetPath(string(match[1]))
		if !ok {
			continue
		}

		stat, err := fs.Stat(distFS, assetPath)
		if err != nil {
			return fmt.Errorf("panel bundle is invalid: missing asset %q referenced by index.html: %w", assetPath, err)
		}
		if stat.IsDir() {
			return fmt.Errorf("panel bundle is invalid: asset %q referenced by index.html is a directory", assetPath)
		}
	}

	return nil
}

func prepareDevelopmentDistAt(ctx context.Context, paths sourcePaths) error {
	if _, err := os.Stat(filepath.Join(paths.panelDir, "package.json")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect panel source: %w", err)
	}

	distFS, err := dirFS(paths.distDir)
	if err == nil && ValidateDist(distFS) == nil {
		return nil
	}

	if err := runPanelBuild(ctx, paths.panelDir); err != nil {
		return fmt.Errorf("rebuild panel bundle: %w", err)
	}

	distFS, err = dirFS(paths.distDir)
	if err != nil {
		return fmt.Errorf("open rebuilt panel bundle: %w", err)
	}
	if err := ValidateDist(distFS); err != nil {
		return fmt.Errorf("validate rebuilt panel bundle: %w", err)
	}

	return nil
}

func dirFS(root string) (fs.FS, error) {
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		return nil, err
	}

	return os.DirFS(root), nil
}

func localSourcePaths() (sourcePaths, bool) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return sourcePaths{}, false
	}

	webDir := filepath.Dir(currentFile)
	paths := sourcePaths{
		distDir:  filepath.Join(webDir, "dist"),
		panelDir: filepath.Join(webDir, "panel"),
	}

	if _, err := os.Stat(paths.panelDir); err != nil {
		return sourcePaths{}, false
	}

	return paths, true
}

func normalizeAssetPath(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" ||
		strings.HasPrefix(ref, "#") ||
		strings.HasPrefix(ref, "http://") ||
		strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "//") ||
		strings.HasPrefix(ref, "data:") {
		return "", false
	}

	ref = strings.SplitN(ref, "#", 2)[0]
	ref = strings.SplitN(ref, "?", 2)[0]
	ref = strings.TrimPrefix(ref, "./")
	ref = strings.TrimPrefix(ref, "/")
	if ref == "" || ref == "index.html" {
		return "", false
	}

	return ref, true
}
