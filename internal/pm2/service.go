package pm2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	listCommandTimeout   = 5 * time.Second
	logsCommandTimeout   = 5 * time.Second
	actionCommandTimeout = 10 * time.Second
	managedNodeRoot      = "/usr/local/nodejs"
	pm2LogMaxSize        = "100M"
	pm2LogLines          = 200
)

var pm2VersionPattern = regexp.MustCompile(`\b(\d+(?:\.\d+)+)\b`)

type Manager interface {
	Status(context.Context) Status
	Sync(context.Context) ([]Process, error)
	List(context.Context) ([]Process, error)
	Logs(context.Context, int) (string, error)
	ClearLogs(context.Context, int) error
	CreateProcess(context.Context, CreateProcessInput) ([]Process, error)
	StartProcess(context.Context, int) ([]Process, error)
	StopProcess(context.Context, int) ([]Process, error)
	RestartProcess(context.Context, int) ([]Process, error)
	DeleteProcess(context.Context, int) ([]Process, error)
	Install(context.Context) error
	Remove(context.Context) error
}

type CreateProcessInput struct {
	Name             string
	ScriptPath       string
	WorkingDirectory string
	Interpreter      string
}

type Process struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	CPU              float64 `json:"cpu"`
	MemoryBytes      int64   `json:"memory_bytes"`
	Restarts         int     `json:"restarts"`
	UptimeUnixMilli  int64   `json:"uptime_unix_milli,omitempty"`
	ScriptPath       string  `json:"script_path,omitempty"`
	WorkingDirectory string  `json:"working_directory,omitempty"`
	Interpreter      string  `json:"interpreter,omitempty"`
	Namespace        string  `json:"namespace,omitempty"`
	Version          string  `json:"version,omitempty"`
	ExecMode         string  `json:"exec_mode,omitempty"`
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Installed        bool     `json:"installed"`
	BinaryPath       string   `json:"binary_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
	RemoveAvailable  bool     `json:"remove_available"`
	RemoveLabel      string   `json:"remove_label,omitempty"`
}

type Service struct {
	logger *zap.Logger
	store  *Store
}

type definitionUpdate struct {
	manuallyStopped *bool
	remove          bool
}

type rawProcess struct {
	Name   string `json:"name"`
	PMID   int    `json:"pm_id"`
	PM2Env struct {
		Status      string `json:"status"`
		PMXModule   bool   `json:"pmx_module"`
		RestartTime int    `json:"restart_time"`
		PMUptime    int64  `json:"pm_uptime"`
		PMExecPath  string `json:"pm_exec_path"`
		PMCwd       string `json:"pm_cwd"`
		Interpreter string `json:"exec_interpreter"`
		Namespace   string `json:"namespace"`
		Version     string `json:"version"`
		ExecMode    string `json:"exec_mode"`
	} `json:"pm2_env"`
	Monit struct {
		Memory int64   `json:"memory"`
		CPU    float64 `json:"cpu"`
	} `json:"monit"`
}

type inspectedProcess struct {
	Process
	Definition Definition
}

func NewService(logger *zap.Logger, store *Store) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
		store:  store,
	}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{
		Platform:       runtime.GOOS,
		PackageManager: "npm",
		InstallLabel:   "Install PM2",
		RemoveLabel:    "Remove PM2",
	}

	nodePath, nodeInstalled := detectNodeBinary()
	_, npmInstalled := detectNPMBinary()
	pm2Path, pm2Installed := detectPM2Binary()
	if pm2Installed {
		status.Installed = true
		status.BinaryPath = pm2Path
		if output, err := runInspectCommand(ctx, pm2Path, "--version"); err == nil {
			status.Version = parseVersion(output)
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}

	status.InstallAvailable = nodeInstalled && npmInstalled && !status.Installed
	status.RemoveAvailable = status.Installed && npmInstalled

	switch {
	case status.Installed && status.Version != "" && status.BinaryPath != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("PM2 %s is installed at %s.", status.Version, status.BinaryPath)
	case status.Installed && status.Version != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("PM2 %s is installed.", status.Version)
	case status.Installed:
		status.State = "installed"
		status.Message = fmt.Sprintf("PM2 is installed at %s.", status.BinaryPath)
	case !nodeInstalled:
		status.State = "missing"
		status.Message = "Install Node.js before installing PM2."
	case !npmInstalled:
		status.State = "missing"
		status.Message = "npm was not detected. Install or repair Node.js before installing PM2."
	case nodePath != "":
		status.State = "missing"
		status.Message = fmt.Sprintf("PM2 is not installed. Node.js is available at %s.", nodePath)
	default:
		status.State = "missing"
		status.Message = "PM2 is not installed."
	}

	if status.Installed && !npmInstalled {
		status.Issues = append(status.Issues, "npm was not detected, so automatic PM2 removal is unavailable.")
	}
	if len(status.Issues) == 0 {
		status.Issues = nil
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	if status := s.Status(ctx); status.Installed {
		return nil
	}

	nodePath, nodeInstalled := detectNodeBinary()
	if !nodeInstalled {
		return errors.New("Node.js must be installed before PM2 can be installed")
	}

	npmPath, npmInstalled := detectNPMBinary()
	if !npmInstalled {
		return errors.New("npm was not detected. Install or repair Node.js before installing PM2")
	}

	s.logger.Info("installing pm2 runtime",
		zap.String("node_path", nodePath),
		zap.String("npm_path", npmPath),
	)
	if err := runCommands(ctx,
		[]string{npmPath, "install", "-g", "pm2"},
	); err != nil {
		return err
	}

	pm2Path, installed := detectPM2Binary()
	if !installed {
		return errors.New("pm2 binary was not found after installation")
	}

	s.logger.Info("configuring pm2 log rotation",
		zap.String("pm2_path", pm2Path),
		zap.String("max_size", pm2LogMaxSize),
	)
	return runCommands(ctx,
		[]string{pm2Path, "install", "pm2-logrotate"},
		[]string{pm2Path, "completion", "install"},
		[]string{pm2Path, "set", "pm2-logrotate:max_size", pm2LogMaxSize},
	)
}

func (s *Service) Sync(ctx context.Context) ([]Process, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return nil, nil
	}

	return s.sync(ctx, pm2Path)
}

func (s *Service) List(ctx context.Context) ([]Process, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return nil, errors.New("PM2 is not installed")
	}

	return s.refresh(ctx, pm2Path, nil)
}

func (s *Service) Logs(ctx context.Context, processID int) (string, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return "", errors.New("PM2 is not installed")
	}
	if processID < 0 {
		return "", errors.New("PM2 logs are unavailable until the saved process is started")
	}

	return runInspectCommandWithTimeout(
		ctx,
		logsCommandTimeout,
		pm2Path,
		"logs",
		strconv.Itoa(processID),
		"--lines",
		strconv.Itoa(pm2LogLines),
		"--nostream",
		"--raw",
	)
}

func (s *Service) ClearLogs(ctx context.Context, processID int) error {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return errors.New("PM2 is not installed")
	}
	if processID < 0 {
		return errors.New("PM2 logs are unavailable until the saved process is started")
	}
	if _, err := s.inspectProcess(ctx, pm2Path, processID); err != nil {
		return err
	}
	if _, err := runInspectCommandWithTimeout(ctx, actionCommandTimeout, pm2Path, "flush", strconv.Itoa(processID)); err != nil {
		return err
	}

	return nil
}

func (s *Service) CreateProcess(ctx context.Context, input CreateProcessInput) ([]Process, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return nil, errors.New("PM2 is not installed")
	}

	scriptPath := strings.TrimSpace(input.ScriptPath)
	if scriptPath == "" {
		return nil, errors.New("PM2 script path is required")
	}

	args := []string{"start", scriptPath}
	if name := strings.TrimSpace(input.Name); name != "" {
		args = append(args, "--name", name)
	}
	if workingDirectory := strings.TrimSpace(input.WorkingDirectory); workingDirectory != "" {
		args = append(args, "--cwd", workingDirectory)
	}
	if interpreter := strings.TrimSpace(input.Interpreter); interpreter != "" {
		args = append(args, "--interpreter", interpreter)
	}

	if _, err := runInspectCommandWithTimeout(ctx, actionCommandTimeout, pm2Path, args...); err != nil {
		return nil, err
	}

	processes, err := s.refresh(ctx, pm2Path, nil)
	if err != nil {
		return nil, err
	}

	return processes, s.saveProcessSnapshot(ctx, pm2Path)
}

func (s *Service) StartProcess(ctx context.Context, processID int) ([]Process, error) {
	if processID < 0 {
		return s.startStoredProcess(ctx, processID)
	}

	return s.runProcessAction(ctx, "start", processID, definitionUpdate{manuallyStopped: boolPointer(false)})
}

func (s *Service) StopProcess(ctx context.Context, processID int) ([]Process, error) {
	if processID < 0 {
		return nil, errors.New("saved stopped processes cannot be stopped again")
	}

	return s.runProcessAction(ctx, "stop", processID, definitionUpdate{manuallyStopped: boolPointer(true)})
}

func (s *Service) RestartProcess(ctx context.Context, processID int) ([]Process, error) {
	if processID < 0 {
		return nil, errors.New("saved stopped processes must be started before they can be restarted")
	}

	return s.runProcessAction(ctx, "restart", processID, definitionUpdate{manuallyStopped: boolPointer(false)})
}

func (s *Service) DeleteProcess(ctx context.Context, processID int) ([]Process, error) {
	if processID < 0 {
		return s.deleteStoredProcess(ctx, processID)
	}

	return s.runProcessAction(ctx, "delete", processID, definitionUpdate{remove: true})
}

func (s *Service) Remove(ctx context.Context) error {
	if status := s.Status(ctx); !status.Installed {
		return nil
	}

	npmPath, installed := detectNPMBinary()
	if !installed {
		return errors.New("npm was not detected. Install or repair Node.js before removing PM2")
	}

	s.logger.Info("removing pm2 runtime",
		zap.String("npm_path", npmPath),
	)
	return runCommands(ctx,
		[]string{npmPath, "uninstall", "-g", "pm2"},
	)
}

func parseVersion(output string) string {
	match := pm2VersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) < 2 {
		return ""
	}

	return strings.TrimSpace(match[1])
}

func (s *Service) sync(ctx context.Context, pm2Path string) ([]Process, error) {
	storedDefinitions, err := s.loadDefinitions(ctx)
	if err != nil {
		return nil, err
	}

	inspected, err := s.inspectProcesses(ctx, pm2Path)
	if err != nil {
		return nil, err
	}

	missing := missingDefinitions(storedDefinitions, inspected)
	for _, definition := range missing {
		if err := s.createMissingProcess(ctx, pm2Path, definition); err != nil {
			label := strings.TrimSpace(definition.Name)
			if label == "" {
				label = strings.TrimSpace(definition.ScriptPath)
			}
			if label == "" {
				label = "stored PM2 process"
			}
			return nil, fmt.Errorf("restore %s: %w", label, err)
		}
	}

	if len(storedDefinitions) > 0 && len(missing) > 0 {
		processes, err := s.refresh(ctx, pm2Path, nil)
		if err != nil {
			return nil, err
		}

		return processes, s.saveProcessSnapshot(ctx, pm2Path)
	}

	return s.persistDefinitions(ctx, storedDefinitions, inspected, nil)
}

func parseInspectedProcesses(output string) ([]inspectedProcess, error) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, nil
	}

	var records []rawProcess
	if err := json.Unmarshal([]byte(trimmed), &records); err != nil {
		return nil, fmt.Errorf("failed to parse PM2 process list: %w", err)
	}

	processes := make([]inspectedProcess, 0, len(records))
	for _, record := range records {
		if isModuleProcess(record) {
			continue
		}

		process := Process{
			ID:               record.PMID,
			Name:             strings.TrimSpace(record.Name),
			Status:           strings.TrimSpace(record.PM2Env.Status),
			CPU:              record.Monit.CPU,
			MemoryBytes:      record.Monit.Memory,
			Restarts:         record.PM2Env.RestartTime,
			UptimeUnixMilli:  record.PM2Env.PMUptime,
			ScriptPath:       strings.TrimSpace(record.PM2Env.PMExecPath),
			WorkingDirectory: strings.TrimSpace(record.PM2Env.PMCwd),
			Interpreter:      strings.TrimSpace(record.PM2Env.Interpreter),
			Namespace:        strings.TrimSpace(record.PM2Env.Namespace),
			Version:          strings.TrimSpace(record.PM2Env.Version),
			ExecMode:         strings.TrimSpace(record.PM2Env.ExecMode),
		}
		definition := Definition{
			Name:             strings.TrimSpace(record.Name),
			ScriptPath:       process.ScriptPath,
			WorkingDirectory: process.WorkingDirectory,
			Interpreter:      process.Interpreter,
		}
		if process.Name == "" {
			process.Name = fmt.Sprintf("Process %d", process.ID)
		}
		processes = append(processes, inspectedProcess{
			Process:    process,
			Definition: definition,
		})
	}

	sort.Slice(processes, func(i, j int) bool {
		leftName := strings.ToLower(processes[i].Process.Name)
		rightName := strings.ToLower(processes[j].Process.Name)
		if leftName == rightName {
			return processes[i].Process.ID < processes[j].Process.ID
		}
		return leftName < rightName
	})

	return processes, nil
}

func toProcesses(inspected []inspectedProcess, stored []Definition, matchedStoredCounts map[string]int) []Process {
	processes := make([]Process, 0, len(inspected))
	for _, process := range inspected {
		processes = append(processes, process.Process)
	}
	if len(stored) == 0 {
		return processes
	}

	virtualID := -1
	remainingMatches := cloneDefinitionCounts(matchedStoredCounts)
	for _, definition := range stored {
		key := definitionKey(definition)
		if remainingMatches[key] > 0 {
			remainingMatches[key]--
			continue
		}
		if !definition.ManuallyStopped {
			continue
		}

		name := strings.TrimSpace(definition.Name)
		if name == "" {
			name = strings.TrimSpace(definition.ScriptPath)
		}
		if name == "" {
			name = fmt.Sprintf("Process %d", virtualID)
		}
		processes = append(processes, Process{
			ID:               virtualID,
			Name:             name,
			Status:           "stopped",
			ScriptPath:       strings.TrimSpace(definition.ScriptPath),
			WorkingDirectory: strings.TrimSpace(definition.WorkingDirectory),
			Interpreter:      strings.TrimSpace(definition.Interpreter),
		})
		virtualID--
	}

	return processes
}

func isModuleProcess(process rawProcess) bool {
	if process.PM2Env.PMXModule {
		return true
	}

	scriptPath := filepath.ToSlash(strings.TrimSpace(process.PM2Env.PMExecPath))
	return strings.Contains(scriptPath, "/.pm2/modules/")
}

func (s *Service) runProcessAction(ctx context.Context, action string, processID int, update definitionUpdate) ([]Process, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return nil, errors.New("PM2 is not installed")
	}

	target, err := s.inspectProcess(ctx, pm2Path, processID)
	if err != nil {
		return nil, err
	}

	if _, err := runInspectCommandWithTimeout(ctx, actionCommandTimeout, pm2Path, action, strconv.Itoa(processID)); err != nil {
		return nil, err
	}

	processes, err := s.refresh(ctx, pm2Path, map[string][]definitionUpdate{
		definitionKey(target.Definition): {update},
	})
	if err != nil {
		return nil, err
	}

	return processes, s.saveProcessSnapshot(ctx, pm2Path)
}

func (s *Service) inspectProcesses(ctx context.Context, pm2Path string) ([]inspectedProcess, error) {
	output, err := runInspectCommandWithTimeout(ctx, listCommandTimeout, pm2Path, "jlist")
	if err != nil {
		return nil, err
	}

	return parseInspectedProcesses(output)
}

func (s *Service) refresh(ctx context.Context, pm2Path string, updates map[string][]definitionUpdate) ([]Process, error) {
	storedDefinitions, err := s.loadDefinitions(ctx)
	if err != nil {
		return nil, err
	}

	inspected, err := s.inspectProcesses(ctx, pm2Path)
	if err != nil {
		return nil, err
	}

	return s.persistDefinitions(ctx, storedDefinitions, inspected, updates)
}

func (s *Service) loadDefinitions(ctx context.Context) ([]Definition, error) {
	if s.store == nil {
		return nil, nil
	}

	definitions, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load pm2 process definitions: %w", err)
	}

	return definitions, nil
}

func (s *Service) replaceDefinitions(ctx context.Context, definitions []Definition) error {
	if s.store == nil {
		return nil
	}

	if err := s.store.Replace(ctx, definitions); err != nil {
		return fmt.Errorf("persist pm2 process definitions: %w", err)
	}

	return nil
}

func (s *Service) persistDefinitions(ctx context.Context, stored []Definition, inspected []inspectedProcess, updates map[string][]definitionUpdate) ([]Process, error) {
	matchedStoredCounts := preserveStoredDefinitionState(inspected, stored)
	definitions := mergeDefinitions(stored, inspected, cloneDefinitionCounts(matchedStoredCounts), updates)
	processes := toProcesses(inspected, definitions, cloneDefinitionCounts(matchedStoredCounts))

	if err := s.replaceDefinitions(ctx, definitions); err != nil {
		return nil, err
	}

	return processes, nil
}

func missingDefinitions(stored []Definition, inspected []inspectedProcess) []Definition {
	if len(stored) == 0 {
		return nil
	}

	liveCounts := make(map[string]int, len(inspected))
	for _, process := range inspected {
		liveCounts[definitionKey(process.Definition)]++
	}

	missing := make([]Definition, 0)
	for _, definition := range stored {
		key := definitionKey(definition)
		if liveCounts[key] > 0 {
			liveCounts[key]--
			continue
		}
		if definition.ManuallyStopped {
			continue
		}
		missing = append(missing, definition)
	}

	return missing
}

func definitionKey(definition Definition) string {
	return strings.TrimSpace(definition.Name) + "\x00" +
		strings.TrimSpace(definition.ScriptPath) + "\x00" +
		strings.TrimSpace(definition.WorkingDirectory) + "\x00" +
		strings.TrimSpace(definition.Interpreter)
}

func preserveStoredDefinitionState(inspected []inspectedProcess, stored []Definition) map[string]int {
	if len(inspected) == 0 || len(stored) == 0 {
		return nil
	}

	storedQueues := make(map[string][]Definition, len(stored))
	for _, definition := range stored {
		key := definitionKey(definition)
		storedQueues[key] = append(storedQueues[key], definition)
	}

	matchedCounts := make(map[string]int, len(inspected))
	for i := range inspected {
		key := definitionKey(inspected[i].Definition)
		queue := storedQueues[key]
		if len(queue) == 0 {
			continue
		}
		inspected[i].Definition.ManuallyStopped = queue[0].ManuallyStopped
		storedQueues[key] = queue[1:]
		matchedCounts[key]++
	}

	return matchedCounts
}

func mergeDefinitions(stored []Definition, inspected []inspectedProcess, matchedStoredCounts map[string]int, updates map[string][]definitionUpdate) []Definition {
	definitions := make([]Definition, 0, len(stored)+len(inspected))
	for _, process := range inspected {
		definition, include := applyDefinitionUpdate(process.Definition, updates)
		if include {
			definitions = append(definitions, definition)
		}
	}

	for _, definition := range stored {
		key := definitionKey(definition)
		if matchedStoredCounts[key] > 0 {
			matchedStoredCounts[key]--
			continue
		}
		definition, include := applyDefinitionUpdate(definition, updates)
		if include {
			definitions = append(definitions, definition)
		}
	}

	return definitions
}

func applyDefinitionUpdate(definition Definition, updates map[string][]definitionUpdate) (Definition, bool) {
	if len(updates) == 0 {
		return definition, true
	}

	key := definitionKey(definition)
	queue := updates[key]
	if len(queue) == 0 {
		return definition, true
	}

	update := queue[0]
	if len(queue) == 1 {
		delete(updates, key)
	} else {
		updates[key] = queue[1:]
	}
	if update.remove {
		return Definition{}, false
	}
	if update.manuallyStopped != nil {
		definition.ManuallyStopped = *update.manuallyStopped
	}

	return definition, true
}

func boolPointer(value bool) *bool {
	return &value
}

func cloneDefinitionCounts(counts map[string]int) map[string]int {
	if len(counts) == 0 {
		return nil
	}

	cloned := make(map[string]int, len(counts))
	for key, count := range counts {
		cloned[key] = count
	}

	return cloned
}

func (s *Service) createMissingProcess(ctx context.Context, pm2Path string, definition Definition) error {
	scriptPath := strings.TrimSpace(definition.ScriptPath)
	if scriptPath == "" {
		return nil
	}

	args := []string{"start", scriptPath}
	if name := strings.TrimSpace(definition.Name); name != "" {
		args = append(args, "--name", name)
	}
	if workingDirectory := strings.TrimSpace(definition.WorkingDirectory); workingDirectory != "" {
		args = append(args, "--cwd", workingDirectory)
	}
	if interpreter := strings.TrimSpace(definition.Interpreter); interpreter != "" {
		args = append(args, "--interpreter", interpreter)
	}
	if _, err := runInspectCommandWithTimeout(ctx, actionCommandTimeout, pm2Path, args...); err != nil {
		return err
	}

	return nil
}

func (s *Service) inspectProcess(ctx context.Context, pm2Path string, processID int) (inspectedProcess, error) {
	processes, err := s.inspectProcesses(ctx, pm2Path)
	if err != nil {
		return inspectedProcess{}, err
	}

	for _, process := range processes {
		if process.ID == processID {
			return process, nil
		}
	}

	return inspectedProcess{}, fmt.Errorf("PM2 process %d was not found", processID)
}

func (s *Service) startStoredProcess(ctx context.Context, processID int) ([]Process, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return nil, errors.New("PM2 is not installed")
	}

	definition, err := s.resolveStoredProcess(ctx, pm2Path, processID)
	if err != nil {
		return nil, err
	}
	if err := s.createMissingProcess(ctx, pm2Path, definition); err != nil {
		return nil, err
	}

	processes, err := s.refresh(ctx, pm2Path, map[string][]definitionUpdate{
		definitionKey(definition): {{manuallyStopped: boolPointer(false)}},
	})
	if err != nil {
		return nil, err
	}

	return processes, s.saveProcessSnapshot(ctx, pm2Path)
}

func (s *Service) deleteStoredProcess(ctx context.Context, processID int) ([]Process, error) {
	pm2Path, installed := detectPM2Binary()
	if !installed {
		return nil, errors.New("PM2 is not installed")
	}

	definition, err := s.resolveStoredProcess(ctx, pm2Path, processID)
	if err != nil {
		return nil, err
	}

	processes, err := s.refresh(ctx, pm2Path, map[string][]definitionUpdate{
		definitionKey(definition): {{remove: true}},
	})
	if err != nil {
		return nil, err
	}

	return processes, s.saveProcessSnapshot(ctx, pm2Path)
}

func (s *Service) saveProcessSnapshot(ctx context.Context, pm2Path string) error {
	_, err := runInspectCommandWithTimeout(ctx, actionCommandTimeout, pm2Path, "save", "--force")
	return err
}

func (s *Service) resolveStoredProcess(ctx context.Context, pm2Path string, processID int) (Definition, error) {
	storedDefinitions, err := s.loadDefinitions(ctx)
	if err != nil {
		return Definition{}, err
	}
	inspected, err := s.inspectProcesses(ctx, pm2Path)
	if err != nil {
		return Definition{}, err
	}

	matchedStoredCounts := preserveStoredDefinitionState(inspected, storedDefinitions)
	virtualID := -1
	for _, definition := range storedDefinitions {
		key := definitionKey(definition)
		if matchedStoredCounts[key] > 0 {
			matchedStoredCounts[key]--
			continue
		}
		if !definition.ManuallyStopped {
			continue
		}
		if virtualID == processID {
			return definition, nil
		}
		virtualID--
	}

	return Definition{}, fmt.Errorf("saved PM2 process %d was not found", processID)
}

func detectNodeBinary() (string, bool) {
	if path, ok := lookupCommand("node"); ok {
		return path, true
	}

	return lookupCommand("nodejs")
}

func detectNPMBinary() (string, bool) {
	managedPath := filepath.Join(managedNodeRoot, "bin", "npm")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true
	}

	return lookupCommand("npm")
}

func detectPM2Binary() (string, bool) {
	managedPath := filepath.Join(managedNodeRoot, "bin", "pm2")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true
	}

	return lookupCommand("pm2")
}

func lookupCommand(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range []string{
		filepath.Join(managedNodeRoot, "bin"),
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/usr/sbin",
	} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, true
	}

	return "", false
}

func runInspectCommand(ctx context.Context, name string, args ...string) (string, error) {
	return runInspectCommandWithTimeout(ctx, statusCommandTimeout, name, args...)
}

func runInspectCommandWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, timeout)
		defer cancel()
	}

	return runCommand(runCtx, name, args...)
}

func runCommands(ctx context.Context, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if _, err := runCommand(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Env = commandEnv()
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return combinedOutput, fmt.Errorf("%s timed out", name)
	case errors.Is(runCtx.Err(), context.Canceled):
		return combinedOutput, fmt.Errorf("%s was canceled", name)
	case combinedOutput == "":
		return combinedOutput, fmt.Errorf("%s failed: %w", name, err)
	default:
		return combinedOutput, fmt.Errorf("%s failed: %s", name, combinedOutput)
	}
}

func commandEnv() []string {
	pathEntry := filepath.Join(managedNodeRoot, "bin")
	currentPath := strings.TrimSpace(os.Getenv("PATH"))
	for _, entry := range filepath.SplitList(currentPath) {
		if strings.TrimSpace(entry) == pathEntry {
			return os.Environ()
		}
	}

	if currentPath == "" {
		return append(os.Environ(), "PATH="+pathEntry)
	}

	return append(os.Environ(), "PATH="+pathEntry+string(os.PathListSeparator)+currentPath)
}
