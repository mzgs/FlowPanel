package taskmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	flowcron "flowpanel/internal/cron"

	robfigcron "github.com/robfig/cron/v3"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/process"
	"go.uber.org/zap"
)

const actionTimeout = 5 * time.Second

var scheduleParser = robfigcron.NewParser(
	robfigcron.Minute |
		robfigcron.Hour |
		robfigcron.Dom |
		robfigcron.Month |
		robfigcron.Dow |
		robfigcron.Descriptor,
)

type Snapshot struct {
	Platform       string          `json:"platform"`
	Notices        []string        `json:"notices,omitempty"`
	Processes      []ProcessRecord `json:"processes"`
	Services       []ServiceRecord `json:"services"`
	StartupItems   []StartupItem   `json:"startup_items"`
	Users          []UserRecord    `json:"users"`
	ScheduledTasks []ScheduledTask `json:"scheduled_tasks"`
}

type ProcessRecord struct {
	PID             int        `json:"pid"`
	Name            string     `json:"name"`
	User            string     `json:"user,omitempty"`
	State           string     `json:"state,omitempty"`
	Command         string     `json:"command,omitempty"`
	CPUUsagePercent *float64   `json:"cpu_usage_percent,omitempty"`
	MemoryBytes     *uint64    `json:"memory_bytes,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
}

type ServiceRecord struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Manager      string `json:"manager"`
	Description  string `json:"description,omitempty"`
	ActiveState  string `json:"active_state,omitempty"`
	SubState     string `json:"sub_state,omitempty"`
	StartupState string `json:"startup_state,omitempty"`
	User         string `json:"user,omitempty"`
	File         string `json:"file,omitempty"`
	Command      string `json:"command,omitempty"`
	Running      bool   `json:"running"`
}

type StartupItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Manager   string `json:"manager"`
	State     string `json:"state"`
	User      string `json:"user,omitempty"`
	File      string `json:"file,omitempty"`
	Running   bool   `json:"running"`
	Available bool   `json:"available"`
}

type UserRecord struct {
	Username      string     `json:"username"`
	UID           string     `json:"uid,omitempty"`
	GID           string     `json:"gid,omitempty"`
	HomeDirectory string     `json:"home_directory,omitempty"`
	Shell         string     `json:"shell,omitempty"`
	LoggedIn      bool       `json:"logged_in"`
	SessionCount  int        `json:"session_count"`
	Terminals     []string   `json:"terminals,omitempty"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
}

type ScheduledTask struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Source     string     `json:"source"`
	Schedule   string     `json:"schedule"`
	Command    string     `json:"command"`
	State      string     `json:"state"`
	LastStatus string     `json:"last_status,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
}

type Service struct {
	logger *zap.Logger
	cron   *flowcron.Scheduler
}

func NewService(logger *zap.Logger, scheduler *flowcron.Scheduler) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
		cron:   scheduler,
	}
}

func (s *Service) Snapshot(ctx context.Context) Snapshot {
	snapshot := Snapshot{
		Platform:       runtime.GOOS,
		Processes:      []ProcessRecord{},
		Services:       []ServiceRecord{},
		StartupItems:   []StartupItem{},
		Users:          []UserRecord{},
		ScheduledTasks: []ScheduledTask{},
	}

	if processes, err := listProcesses(ctx); err == nil {
		snapshot.Processes = processes
	} else {
		snapshot.Notices = append(snapshot.Notices, err.Error())
	}

	snapshot.Services, snapshot.StartupItems, snapshot.Notices = loadRuntimeEntries(ctx, snapshot.Notices)
	snapshot.Users, snapshot.Notices = loadUsers(ctx, snapshot.Notices)

	snapshot.ScheduledTasks = listScheduledTasks(s.cron)
	return snapshot
}

func (s *Service) TerminateProcess(ctx context.Context, pid int) error {
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return fmt.Errorf("inspect process %d: %w", pid, err)
	}

	actionCtx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	if err := proc.TerminateWithContext(actionCtx); err != nil {
		return fmt.Errorf("terminate process %d: %w", pid, err)
	}

	return nil
}

func (s *Service) StartService(ctx context.Context, id string) error {
	return runServiceAction(ctx, "start", id)
}

func (s *Service) StopService(ctx context.Context, id string) error {
	return runServiceAction(ctx, "stop", id)
}

func (s *Service) RestartService(ctx context.Context, id string) error {
	return runServiceAction(ctx, "restart", id)
}

func (s *Service) EnableStartupItem(ctx context.Context, id string) error {
	return runStartupAction(ctx, "enable", id)
}

func (s *Service) DisableStartupItem(ctx context.Context, id string) error {
	return runStartupAction(ctx, "disable", id)
}

func listProcesses(ctx context.Context) ([]ProcessRecord, error) {
	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("load processes: %w", err)
	}

	records := make([]ProcessRecord, 0, len(processes))
	for _, proc := range processes {
		if proc == nil {
			continue
		}

		record := ProcessRecord{PID: int(proc.Pid)}
		record.Name, _ = proc.NameWithContext(ctx)
		record.User, _ = proc.UsernameWithContext(ctx)
		record.Command, _ = proc.CmdlineWithContext(ctx)
		if record.Name == "" && record.Command != "" {
			commandFields := strings.Fields(record.Command)
			if len(commandFields) > 0 {
				record.Name = filepath.Base(commandFields[0])
			}
		}
		if states, err := proc.StatusWithContext(ctx); err == nil && len(states) > 0 {
			record.State = strings.Join(states, ", ")
		}
		if cpuUsage, err := proc.CPUPercentWithContext(ctx); err == nil {
			cpuUsage = math.Round(cpuUsage*10) / 10
			record.CPUUsagePercent = &cpuUsage
		}
		if memoryInfo, err := proc.MemoryInfoWithContext(ctx); err == nil && memoryInfo != nil {
			memoryBytes := memoryInfo.RSS
			record.MemoryBytes = &memoryBytes
		}
		if createdAtMillis, err := proc.CreateTimeWithContext(ctx); err == nil && createdAtMillis > 0 {
			createdAt := time.UnixMilli(createdAtMillis).UTC()
			record.StartedAt = &createdAt
		}
		if record.Name == "" {
			record.Name = fmt.Sprintf("Process %d", record.PID)
		}

		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		leftCPU := numericPtrValue(records[i].CPUUsagePercent)
		rightCPU := numericPtrValue(records[j].CPUUsagePercent)
		if leftCPU != rightCPU {
			return leftCPU > rightCPU
		}

		leftMemory := uint64PtrValue(records[i].MemoryBytes)
		rightMemory := uint64PtrValue(records[j].MemoryBytes)
		if leftMemory != rightMemory {
			return leftMemory > rightMemory
		}

		return records[i].PID < records[j].PID
	})

	return records, nil
}

func listRuntimeEntries(ctx context.Context) ([]ServiceRecord, []StartupItem, []string) {
	switch runtime.GOOS {
	case "linux":
		return listSystemdEntries(ctx)
	case "darwin":
		return listHomebrewEntries(ctx)
	default:
		return []ServiceRecord{}, []StartupItem{}, []string{"Service and startup item management is unavailable on this platform."}
	}
}

func listSystemdEntries(ctx context.Context) ([]ServiceRecord, []StartupItem, []string) {
	systemctlPath, err := exec.LookPath("systemctl")
	if err != nil {
		return []ServiceRecord{}, []StartupItem{}, []string{"systemctl is not available on this node."}
	}

	serviceMap := make(map[string]*ServiceRecord)
	servicesOutput, serviceErr := runCommand(ctx, systemctlPath, "list-units", "--type=service", "--all", "--plain", "--no-legend", "--no-pager")
	if serviceErr == nil {
		for _, line := range nonEmptyLines(servicesOutput) {
			fields := splitFieldsN(line, 5)
			if len(fields) < 5 {
				continue
			}

			record := &ServiceRecord{
				ID:          fields[0],
				Name:        strings.TrimSuffix(fields[0], ".service"),
				Manager:     "systemd",
				ActiveState: fields[2],
				SubState:    fields[3],
				Description: fields[4],
				Running:     fields[2] == "active",
			}
			serviceMap[record.ID] = record
		}
	}

	unitFilesOutput, unitFilesErr := runCommand(ctx, systemctlPath, "list-unit-files", "--type=service", "--all", "--no-legend", "--no-pager")
	startupItems := make([]StartupItem, 0)
	if unitFilesErr == nil {
		for _, line := range nonEmptyLines(unitFilesOutput) {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}

			id := fields[0]
			state := fields[1]
			record := ensureServiceRecord(serviceMap, id, "systemd")
			record.StartupState = state

			startupItems = append(startupItems, StartupItem{
				ID:        id,
				Name:      record.Name,
				Manager:   "systemd",
				State:     state,
				Running:   record.Running,
				Available: true,
			})
		}
	}

	services := make([]ServiceRecord, 0, len(serviceMap))
	for _, record := range serviceMap {
		services = append(services, *record)
	}

	sortByName(services, func(item ServiceRecord) string { return item.Name })
	sortByName(startupItems, func(item StartupItem) string { return item.Name })

	return services, startupItems, collectErrors(serviceErr, unitFilesErr)
}

type homebrewServiceInfo struct {
	Name        string `json:"name"`
	ServiceName string `json:"service_name"`
	Running     bool   `json:"running"`
	PID         *int   `json:"pid"`
	User        string `json:"user"`
	Status      string `json:"status"`
	File        string `json:"file"`
	Registered  bool   `json:"registered"`
	Command     string `json:"command"`
}

func listHomebrewEntries(ctx context.Context) ([]ServiceRecord, []StartupItem, []string) {
	brewPath, err := exec.LookPath("brew")
	if err != nil {
		return []ServiceRecord{}, []StartupItem{}, []string{"Homebrew is not available on this node."}
	}

	output, err := runCommand(ctx, brewPath, "services", "info", "--all", "--json")
	if err != nil {
		return []ServiceRecord{}, []StartupItem{}, []string{err.Error()}
	}

	var items []homebrewServiceInfo
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return []ServiceRecord{}, []StartupItem{}, []string{fmt.Sprintf("parse Homebrew services: %v", err)}
	}

	services := make([]ServiceRecord, 0, len(items))
	startupItems := make([]StartupItem, 0, len(items))
	for _, item := range items {
		startupState := "disabled"
		if item.Registered {
			startupState = "enabled"
		}

		services = append(services, ServiceRecord{
			ID:           item.Name,
			Name:         item.Name,
			Manager:      "homebrew",
			Description:  item.ServiceName,
			ActiveState:  item.Status,
			StartupState: startupState,
			User:         item.User,
			File:         item.File,
			Command:      item.Command,
			Running:      item.Running,
		})
		startupItems = append(startupItems, StartupItem{
			ID:        item.Name,
			Name:      item.Name,
			Manager:   "homebrew",
			State:     startupState,
			User:      item.User,
			File:      item.File,
			Running:   item.Running,
			Available: true,
		})
	}

	sortByName(services, func(item ServiceRecord) string { return item.Name })
	sortByName(startupItems, func(item StartupItem) string { return item.Name })

	return services, startupItems, nil
}

func listUsers(ctx context.Context) ([]UserRecord, []string) {
	userMap := make(map[string]*UserRecord)
	notices := make([]string, 0, 2)

	for _, user := range listLocalUsers(ctx, &notices) {
		record := user
		userMap[record.Username] = &record
	}

	sessions, err := host.UsersWithContext(ctx)
	if err != nil {
		notices = append(notices, fmt.Sprintf("load active users: %v", err))
	} else {
		for _, session := range sessions {
			username := strings.TrimSpace(session.User)
			if username == "" {
				continue
			}

			record := ensureUserRecord(userMap, username)

			record.LoggedIn = true
			record.SessionCount++
			if terminal := strings.TrimSpace(session.Terminal); terminal != "" && !containsString(record.Terminals, terminal) {
				record.Terminals = append(record.Terminals, terminal)
			}
			if session.Started > 0 {
				lastSeenAt := time.Unix(int64(session.Started), 0).UTC()
				if record.LastSeenAt == nil || lastSeenAt.After(*record.LastSeenAt) {
					record.LastSeenAt = &lastSeenAt
				}
			}
		}
	}

	users := make([]UserRecord, 0, len(userMap))
	for _, record := range userMap {
		users = append(users, *record)
	}

	sort.Slice(users, func(i, j int) bool {
		if users[i].LoggedIn != users[j].LoggedIn {
			return users[i].LoggedIn
		}
		return strings.ToLower(users[i].Username) < strings.ToLower(users[j].Username)
	})

	return users, notices
}

func listLocalUsers(ctx context.Context, notices *[]string) []UserRecord {
	var (
		users []UserRecord
		err   error
	)

	switch runtime.GOOS {
	case "darwin":
		users, err = listMacUsers(ctx)
	case "linux":
		users, err = listPasswdUsers("/etc/passwd", 1000)
	}

	if err == nil {
		return users
	}

	appendError(notices, err)
	return []UserRecord{}
}

func listMacUsers(ctx context.Context) ([]UserRecord, error) {
	dsclPath, err := exec.LookPath("dscl")
	if err != nil {
		return nil, fmt.Errorf("dscl is not available for local user lookup")
	}

	output, err := runCommand(ctx, dsclPath, ".", "-list", "/Users", "UniqueID", "PrimaryGroupID", "NFSHomeDirectory", "UserShell")
	if err != nil {
		return nil, err
	}

	users := make([]UserRecord, 0)
	for _, line := range nonEmptyLines(output) {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		username := fields[0]
		uidValue := fields[1]
		uid, err := strconv.Atoi(uidValue)
		if err != nil || !includeUser(username, uid, 500) {
			continue
		}

		users = append(users, UserRecord{
			Username:      username,
			UID:           uidValue,
			GID:           fields[2],
			HomeDirectory: fields[3],
			Shell:         fields[4],
		})
	}

	return users, nil
}

func listPasswdUsers(path string, minUID int) ([]UserRecord, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	users := make([]UserRecord, 0)
	for _, line := range nonEmptyLines(string(content)) {
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}

		username := fields[0]
		uidValue := fields[2]
		uid, err := strconv.Atoi(uidValue)
		if err != nil || !includeUser(username, uid, minUID) {
			continue
		}

		users = append(users, UserRecord{
			Username:      username,
			UID:           uidValue,
			GID:           fields[3],
			HomeDirectory: fields[5],
			Shell:         fields[6],
		})
	}

	return users, nil
}

func includeUser(username string, uid int, minUID int) bool {
	username = strings.TrimSpace(username)
	if username == "" {
		return false
	}

	return username == "root" || uid >= minUID
}

func listScheduledTasks(scheduler *flowcron.Scheduler) []ScheduledTask {
	if scheduler == nil {
		return []ScheduledTask{}
	}

	snapshot := scheduler.Snapshot()
	tasks := make([]ScheduledTask, 0, len(snapshot.Jobs))
	now := time.Now()
	baseState := "stopped"
	if snapshot.Enabled {
		baseState = "scheduled"
		if !snapshot.Started {
			baseState = "pending"
		}
	}

	for _, job := range snapshot.Jobs {
		task := ScheduledTask{
			ID:       job.ID,
			Name:     job.Name,
			Source:   "flowpanel_cron",
			Schedule: job.Schedule,
			Command:  job.Command,
			State:    baseState,
		}

		if schedule, err := scheduleParser.Parse(job.Schedule); err == nil {
			nextRunAt := schedule.Next(now).UTC()
			task.NextRunAt = &nextRunAt
		}

		if len(job.Executions) > 0 {
			lastExecution := job.Executions[0]
			task.LastStatus = lastExecution.Status
			lastRunAt := lastExecution.StartedAt.UTC()
			task.LastRunAt = &lastRunAt
		}

		tasks = append(tasks, task)
	}

	sortByName(tasks, func(item ScheduledTask) string { return item.Name })

	return tasks
}

func runServiceAction(ctx context.Context, action string, id string) error {
	return runPlatformAction(ctx, strings.TrimSpace(id), "service id is required", "service management", []string{"systemctl", action}, []string{"brew", "services", action})
}

func runStartupAction(ctx context.Context, action string, id string) error {
	darwinAction := "start"
	if action == "disable" {
		darwinAction = "stop"
	}
	return runPlatformAction(ctx, strings.TrimSpace(id), "startup item id is required", "startup item management", []string{"systemctl", action}, []string{"brew", "services", darwinAction})
}

func runManagedCommand(ctx context.Context, name string, args ...string) error {
	commandPath, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s is not available", name)
	}

	actionCtx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()

	_, err = runCommand(actionCtx, commandPath, args...)
	return err
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), message)
	}

	return string(output), nil
}

func splitFieldsN(value string, limit int) []string {
	fields := strings.Fields(value)
	if len(fields) <= limit {
		return fields
	}

	head := append([]string{}, fields[:limit-1]...)
	return append(head, strings.Join(fields[limit-1:], " "))
}

func nonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

func loadRuntimeEntries(ctx context.Context, notices []string) ([]ServiceRecord, []StartupItem, []string) {
	services, startupItems, runtimeNotices := listRuntimeEntries(ctx)
	return services, startupItems, append(notices, runtimeNotices...)
}

func loadUsers(ctx context.Context, notices []string) ([]UserRecord, []string) {
	users, userNotices := listUsers(ctx)
	return users, append(notices, userNotices...)
}

func collectErrors(errs ...error) []string {
	notices := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			notices = append(notices, err.Error())
		}
	}
	return notices
}

func appendError(notices *[]string, err error) {
	if notices != nil && err != nil {
		*notices = append(*notices, err.Error())
	}
}

func ensureServiceRecord(records map[string]*ServiceRecord, id, manager string) *ServiceRecord {
	if record := records[id]; record != nil {
		return record
	}

	record := &ServiceRecord{
		ID:      id,
		Name:    strings.TrimSuffix(id, ".service"),
		Manager: manager,
	}
	records[id] = record
	return record
}

func ensureUserRecord(records map[string]*UserRecord, username string) *UserRecord {
	if record := records[username]; record != nil {
		return record
	}

	record := &UserRecord{Username: username}
	records[username] = record
	return record
}

func sortByName[T any](items []T, getName func(T) string) {
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(getName(items[i])) < strings.ToLower(getName(items[j]))
	})
}

func runPlatformAction(ctx context.Context, id, emptyIDMessage, unsupportedAction string, linuxArgs, darwinArgs []string) error {
	if id == "" {
		return fmt.Errorf("%s", emptyIDMessage)
	}

	switch runtime.GOOS {
	case "linux":
		return runManagedCommand(ctx, linuxArgs[0], append(linuxArgs[1:], id)...)
	case "darwin":
		return runManagedCommand(ctx, darwinArgs[0], append(darwinArgs[1:], id)...)
	default:
		return fmt.Errorf("%s is unavailable on %s", unsupportedAction, runtime.GOOS)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func numericPtrValue(value *float64) float64 {
	if value == nil {
		return -1
	}

	return *value
}

func uint64PtrValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}

	return *value
}
