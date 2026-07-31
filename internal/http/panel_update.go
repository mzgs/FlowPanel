package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"
)

const latestPanelReleaseURL = "https://api.github.com/repos/mzgs/FlowPanel/releases/latest"

type panelUpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
}

func inspectPanelUpdate(ctx context.Context, currentVersion string) (panelUpdateStatus, error) {
	currentVersion = normalizePanelVersion(currentVersion)
	status := panelUpdateStatus{CurrentVersion: currentVersion}
	if currentVersion == "0.0.0" {
		return status, nil
	}

	request, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, latestPanelReleaseURL, nil)
	if err != nil {
		return status, fmt.Errorf("prepare latest panel release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "FlowPanel")

	client := stdhttp.Client{Timeout: 4 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return status, fmt.Errorf("inspect latest panel release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != stdhttp.StatusOK {
		return status, fmt.Errorf("inspect latest panel release: unexpected status %s", response.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return status, fmt.Errorf("parse latest panel release: %w", err)
	}

	status.LatestVersion = normalizePanelVersion(release.TagName)
	status.UpdateAvailable = comparePanelVersions(status.LatestVersion, currentVersion) > 0
	return status, nil
}

func normalizePanelVersion(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return "0.0.0"
	}
	return version
}

func comparePanelVersions(left, right string) int {
	leftParts := strings.SplitN(left, "-", 2)
	rightParts := strings.SplitN(right, "-", 2)
	leftNumbers := strings.Split(leftParts[0], ".")
	rightNumbers := strings.Split(rightParts[0], ".")
	partCount := max(len(leftNumbers), len(rightNumbers))

	for index := range partCount {
		leftPart := versionNumberPart(leftNumbers, index)
		rightPart := versionNumberPart(rightNumbers, index)
		if leftPart < rightPart {
			return -1
		}
		if leftPart > rightPart {
			return 1
		}
	}

	leftPrerelease := len(leftParts) > 1
	rightPrerelease := len(rightParts) > 1
	if leftPrerelease == rightPrerelease {
		return 0
	}
	if leftPrerelease {
		return -1
	}
	return 1
}

func versionNumberPart(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, _ := strconv.Atoi(parts[index])
	return value
}
