package packageruntime

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	ffmpegReleaseAPI  = "https://api.github.com/repos/BtbN/FFmpeg-Builds/releases/latest"
	ffmpegReleaseURL  = "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/"
	ffmpegBinaryPath  = "/usr/local/bin/ffmpeg"
	ffmpegReleasePath = "/usr/local/bin/.flowpanel-ffmpeg-release"
)

func latestFFmpegRelease(ctx context.Context) (name, digest, version string, err error) {
	arch := map[string]string{"amd64": "linux64", "arm64": "linuxarm64"}[runtime.GOARCH]
	if arch == "" {
		return "", "", "", fmt.Errorf("automatic FFmpeg installation is not supported on linux/%s", runtime.GOARCH)
	}
	release, err := inspectGitHubRelease(ctx, ffmpegReleaseAPI)
	if err != nil {
		return "", "", "", err
	}
	var latestMajor, latestMinor int
	for _, asset := range release.Assets {
		branch, _, ok := strings.Cut(strings.TrimPrefix(asset.Name, "ffmpeg-n"), "-latest-")
		var major, minor int
		if _, err := fmt.Sscanf(branch, "%d.%d", &major, &minor); !ok || err != nil {
			continue
		}
		expected := fmt.Sprintf("ffmpeg-n%d.%d-latest-%s-gpl-%d.%d.tar.xz", major, minor, arch, major, minor)
		if asset.Name != expected || !strings.HasPrefix(asset.Digest, "sha256:") || len(asset.Digest) != 71 {
			continue
		}
		if major > latestMajor || major == latestMajor && minor > latestMinor {
			name, digest, version = asset.Name, asset.Digest, branch
			latestMajor, latestMinor = major, minor
		}
	}
	if name == "" {
		return "", "", "", fmt.Errorf("latest FFmpeg release does not contain a stable full GPL build for %s with a SHA-256 digest", arch)
	}
	return name, digest, version, nil
}

func installLatestFFmpeg(ctx context.Context) error {
	tarPath, ok := lookupCommand("tar")
	if !ok {
		return errors.New("tar with xz support is required to install FFmpeg")
	}
	name, digest, _, err := latestFFmpegRelease(ctx)
	if err != nil {
		return err
	}
	archivePath, err := downloadFile(ctx, ffmpegReleaseURL+name, "ffmpeg-*.tar.xz")
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)
	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, archive)
	if err := errors.Join(copyErr, archive.Close()); err != nil {
		return fmt.Errorf("hash FFmpeg archive: %w", err)
	}
	if fmt.Sprintf("sha256:%x", hash.Sum(nil)) != digest {
		return errors.New("FFmpeg archive SHA-256 digest does not match the GitHub release")
	}
	directory := filepath.Dir(ffmpegBinaryPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(directory, ".ffmpeg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	root := strings.TrimSuffix(name, ".tar.xz")
	if err := runCommands(ctx, []string{tarPath, "-xJf", archivePath, "-C", staging, "--strip-components=2", root + "/bin/ffmpeg", root + "/bin/ffprobe"}); err != nil {
		return fmt.Errorf("extract FFmpeg binaries: %w", err)
	}
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		path := filepath.Join(staging, binary)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("FFmpeg archive does not contain a regular %s binary", binary)
		}
		if err := os.Chmod(path, 0o755); err != nil {
			return err
		}
		if _, err := inspectVersion(ctx, path, "-version"); err != nil {
			return fmt.Errorf("validate %s binary: %w", binary, err)
		}
	}
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if err := os.Rename(filepath.Join(staging, binary), filepath.Join(directory, binary)); err != nil {
			return fmt.Errorf("install %s: %w", binary, err)
		}
	}
	// The rolling "latest" tag stays constant; track the downloaded asset digest.
	return os.WriteFile(ffmpegReleasePath, []byte(digest+"\n"), 0o644)
}

func inspectFFmpegUpdate(ctx context.Context) (string, error) {
	_, digest, version, err := latestFFmpegRelease(ctx)
	if err != nil {
		return "", err
	}
	installed, err := os.ReadFile(ffmpegReleasePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if strings.TrimSpace(string(installed)) == digest {
		return "", nil
	}
	return version, nil
}

func removeFFmpeg(ctx context.Context, definition Definition) error {
	for _, path := range []string{ffmpegBinaryPath, filepath.Join(filepath.Dir(ffmpegBinaryPath), "ffprobe"), ffmpegReleasePath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove FFmpeg file: %w", err)
		}
	}
	definition.Key = "ffmpeg-package"
	return runCommands(ctx, detectActionPlan(definition).removeCmds...)
}
