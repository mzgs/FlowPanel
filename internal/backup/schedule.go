package backup

import (
	"fmt"
	"runtime"
	"strings"
)

const ScheduledCommandMarker = "FLOWPANEL_SCHEDULED_BACKUP=1"

func BuildScheduledCommand(executablePath string, input CreateInput) (string, error) {
	path := strings.TrimSpace(executablePath)
	if path == "" {
		return "", fmt.Errorf("executable path is required")
	}

	args := []string{quoteCommandArg(path), "backup", "create"}
	if input.IncludePanelData {
		args = append(args, "--panel-data")
	}
	if input.IncludeSites {
		args = append(args, "--sites")
	}
	if input.IncludeDatabases {
		args = append(args, "--databases")
	}
	args = append(args, "--location", quoteCommandArg(normalizeLocation(input.Location)))

	return ScheduledCommandMarker + " " + strings.Join(args, " "), nil
}

func ParseScheduledCommand(command string) (CreateInput, bool) {
	normalized := strings.TrimSpace(command)
	if !strings.Contains(normalized, ScheduledCommandMarker) {
		return CreateInput{}, false
	}

	input := CreateInput{
		IncludePanelData: strings.Contains(normalized, "--panel-data"),
		IncludeSites:     strings.Contains(normalized, "--sites"),
		IncludeDatabases: strings.Contains(normalized, "--databases"),
		Location:         parseScheduledLocation(normalized),
	}
	if !input.IncludePanelData && !input.IncludeSites && !input.IncludeDatabases {
		return CreateInput{}, false
	}

	return input, true
}

func quoteCommandArg(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}

	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func parseScheduledLocation(command string) string {
	fields := strings.Fields(command)
	for index := 0; index < len(fields); index++ {
		if fields[index] != "--location" || index+1 >= len(fields) {
			continue
		}

		return normalizeLocation(strings.Trim(fields[index+1], `"'`))
	}

	return LocationLocal
}
