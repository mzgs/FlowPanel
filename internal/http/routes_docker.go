package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const (
	dockerListCommandTimeout   = 5 * time.Second
	dockerLogsCommandTimeout   = 10 * time.Second
	dockerSearchCommandTimeout = 10 * time.Second
	dockerActionCommandTimeout = 30 * time.Second
	dockerCreateCommandTimeout = 2 * time.Minute
	dockerExportCommandTimeout = 2 * time.Minute
	dockerSearchResultLimit    = 100
	dockerLogsTailLines        = 200
)

type dockerContainerListItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	State  string `json:"state"`
}

type dockerPSRecord struct {
	ID     string `json:"ID"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
	Names  string `json:"Names"`
}

type dockerImageListItem struct {
	ID           string `json:"id"`
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
	Size         string `json:"size"`
	CreatedSince string `json:"created_since"`
}

type dockerImageLSRecord struct {
	ID           string `json:"ID"`
	Repository   string `json:"Repository"`
	Tag          string `json:"Tag"`
	Size         string `json:"Size"`
	CreatedSince string `json:"CreatedSince"`
}

type dockerHubSearchImage struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	StarCount   int    `json:"star_count"`
	IsOfficial  bool   `json:"is_official"`
}

type createDockerContainerRequest struct {
	Image string `json:"image"`
}

type saveDockerContainerImageRequest struct {
	Image string `json:"image"`
}

type dockerInspectRecord struct {
	ID         string                   `json:"Id"`
	Name       string                   `json:"Name"`
	Config     dockerInspectConfig      `json:"Config"`
	HostConfig dockerInspectHostConfig  `json:"HostConfig"`
	Mounts     []dockerInspectMount     `json:"Mounts"`
	State      dockerInspectStateRecord `json:"State"`
}

type dockerInspectConfig struct {
	Image        string            `json:"Image"`
	Env          []string          `json:"Env"`
	Entrypoint   []string          `json:"Entrypoint"`
	Cmd          []string          `json:"Cmd"`
	WorkingDir   string            `json:"WorkingDir"`
	User         string            `json:"User"`
	Labels       map[string]string `json:"Labels"`
	Hostname     string            `json:"Hostname"`
	Domainname   string            `json:"Domainname"`
	StopSignal   string            `json:"StopSignal"`
	Tty          bool              `json:"Tty"`
	OpenStdin    bool              `json:"OpenStdin"`
	ExposedPorts map[string]any    `json:"ExposedPorts"`
}

type dockerInspectHostConfig struct {
	Binds           []string                       `json:"Binds"`
	PortBindings    map[string][]dockerPortBinding `json:"PortBindings"`
	RestartPolicy   dockerRestartPolicy            `json:"RestartPolicy"`
	NetworkMode     string                         `json:"NetworkMode"`
	ExtraHosts      []string                       `json:"ExtraHosts"`
	CapAdd          []string                       `json:"CapAdd"`
	CapDrop         []string                       `json:"CapDrop"`
	DNS             []string                       `json:"Dns"`
	DNSSearch       []string                       `json:"DnsSearch"`
	Tmpfs           map[string]string              `json:"Tmpfs"`
	ShmSize         int64                          `json:"ShmSize"`
	AutoRemove      bool                           `json:"AutoRemove"`
	PublishAllPorts bool                           `json:"PublishAllPorts"`
	ReadonlyRootfs  bool                           `json:"ReadonlyRootfs"`
	Privileged      bool                           `json:"Privileged"`
	Init            *bool                          `json:"Init"`
}

type dockerInspectMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type dockerInspectStateRecord struct {
	Running bool `json:"Running"`
}

type dockerPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type dockerRestartPolicy struct {
	Name              string `json:"Name"`
	MaximumRetryCount int    `json:"MaximumRetryCount"`
}

func (a *apiRoutes) registerDockerRoutes(r chi.Router) {
	a.registerPackageRuntimeRoutes(r, "docker", "Docker", a.app.Docker)

	containersHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		containers, err := listDockerContainers(r.Context())
		if err != nil {
			a.app.Logger.Error("list docker containers failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"containers": containers})
	})

	imagesHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		images, err := listDockerImages(r.Context())
		if err != nil {
			a.app.Logger.Error("list docker images failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"images": images})
	})

	searchHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		query := trimmedQuery(r, "query")
		if len(query) < 2 {
			writeValidationFailed(w, map[string]string{
				"query": "Enter at least 2 characters to search Docker Hub.",
			})
			return
		}

		results, err := searchDockerHubImages(r.Context(), query, dockerSearchResultLimit)
		if err != nil {
			a.app.Logger.Error("search docker hub images failed", zap.String("query", query), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"results": results})
	})

	createContainerHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		var input createDockerContainerRequest
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		input.Image = strings.TrimSpace(input.Image)
		if input.Image == "" {
			writeValidationFailed(w, map[string]string{
				"image": "Image is required.",
			})
			return
		}

		actionCtx := backgroundRequestContext(r.Context())
		container, err := createDockerContainer(actionCtx, input.Image)
		if err != nil {
			a.app.Logger.Error("create docker container failed", zap.String("image", input.Image), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "create", "docker_container", input.Image, input.Image, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		label := container.Name
		if label == "" {
			label = input.Image
		}
		a.mutationEvent(
			actionCtx,
			"runtime",
			"create",
			"docker_container",
			container.ID,
			label,
			"succeeded",
			fmt.Sprintf("Created Docker container %q from %q.", label, input.Image),
		)
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"container": container})
	})

	registerContainerAction := func(action string, run func(context.Context, string) (dockerContainerListItem, error)) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.Docker == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
				return
			}

			containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
			if containerID == "" {
				writeValidationFailed(w, map[string]string{
					"container_id": "Container ID is required.",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			container, err := run(actionCtx, containerID)
			if err != nil {
				a.app.Logger.Error(action+" docker container failed", zap.String("container_id", containerID), zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "docker_container", containerID, shortDockerID(containerID), "failed", err.Error())
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
				return
			}

			label := container.Name
			if label == "" {
				label = shortDockerID(containerID)
			}
			resourceID := container.ID
			if resourceID == "" {
				resourceID = containerID
			}

			a.mutationEvent(
				actionCtx,
				"runtime",
				action,
				"docker_container",
				resourceID,
				label,
				"succeeded",
				fmt.Sprintf("%s Docker container %q.", dockerContainerActionPastTense(action), label),
			)
			writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
		}
	}

	deleteContainerHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
		if containerID == "" {
			writeValidationFailed(w, map[string]string{
				"container_id": "Container ID is required.",
			})
			return
		}

		actionCtx := backgroundRequestContext(r.Context())
		label := shortDockerID(containerID)
		if container, err := inspectDockerContainer(actionCtx, containerID); err == nil && strings.TrimSpace(container.Name) != "" {
			label = container.Name
		}

		if err := deleteDockerContainer(actionCtx, containerID); err != nil {
			a.app.Logger.Error("delete docker container failed", zap.String("container_id", containerID), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "delete", "docker_container", containerID, label, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"delete",
			"docker_container",
			containerID,
			label,
			"succeeded",
			fmt.Sprintf("Deleted Docker container %q.", label),
		)
		w.WriteHeader(stdhttp.StatusNoContent)
	})

	recreateContainerHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
		if containerID == "" {
			writeValidationFailed(w, map[string]string{
				"container_id": "Container ID is required.",
			})
			return
		}

		actionCtx := backgroundRequestContext(r.Context())
		container, err := recreateDockerContainer(actionCtx, containerID)
		if err != nil {
			a.app.Logger.Error("recreate docker container failed", zap.String("container_id", containerID), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "recreate", "docker_container", containerID, shortDockerID(containerID), "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		label := container.Name
		if label == "" {
			label = shortDockerID(container.ID)
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"recreate",
			"docker_container",
			container.ID,
			label,
			"succeeded",
			fmt.Sprintf("Recreated Docker container %q.", label),
		)
		writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
	})

	saveContainerImageHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
		if containerID == "" {
			writeValidationFailed(w, map[string]string{
				"container_id": "Container ID is required.",
			})
			return
		}

		var input saveDockerContainerImageRequest
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		input.Image = strings.TrimSpace(input.Image)
		if input.Image == "" {
			writeValidationFailed(w, map[string]string{
				"image": "Image name is required.",
			})
			return
		}

		actionCtx := backgroundRequestContext(r.Context())
		label := shortDockerID(containerID)
		if container, err := inspectDockerContainer(actionCtx, containerID); err == nil && strings.TrimSpace(container.Name) != "" {
			label = container.Name
		}

		if err := saveDockerContainerImage(actionCtx, containerID, input.Image); err != nil {
			a.app.Logger.Error(
				"save docker container as image failed",
				zap.String("container_id", containerID),
				zap.String("image", input.Image),
				zap.Error(err),
			)
			a.mutationEvent(actionCtx, "runtime", "save_image", "docker_image", input.Image, input.Image, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"save_image",
			"docker_image",
			input.Image,
			input.Image,
			"succeeded",
			fmt.Sprintf("Saved Docker container %q as image %q.", label, input.Image),
		)
		w.WriteHeader(stdhttp.StatusNoContent)
	})

	snapshotContainerHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
		if containerID == "" {
			writeValidationFailed(w, map[string]string{
				"container_id": "Container ID is required.",
			})
			return
		}

		commandCtx, cancel := context.WithTimeout(r.Context(), dockerExportCommandTimeout)
		defer cancel()

		container, err := inspectDockerContainer(commandCtx, containerID)
		if err != nil {
			a.app.Logger.Error("inspect docker container for snapshot failed", zap.String("container_id", containerID), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		cmd := exec.CommandContext(commandCtx, "docker", "export", containerID)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			a.app.Logger.Error("create docker export stdout pipe failed", zap.String("container_id", containerID), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "Failed to prepare the Docker snapshot download."})
			return
		}

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Start(); err != nil {
			downloadErr := formatDockerCommandError(stderr.String(), err)
			a.app.Logger.Error("start docker export failed", zap.String("container_id", containerID), zap.Error(downloadErr))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": downloadErr.Error()})
			return
		}

		fileName := dockerSnapshotFileName(container)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		w.Header().Set("Content-Type", "application/x-tar")
		if _, err := io.Copy(w, stdout); err != nil {
			a.app.Logger.Error("stream docker snapshot failed", zap.String("container_id", containerID), zap.Error(err))
		}
		if err := cmd.Wait(); err != nil {
			a.app.Logger.Error("docker export failed", zap.String("container_id", containerID), zap.Error(formatDockerCommandError(stderr.String(), err)))
		}
	})

	containerLogsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		containerID := strings.TrimSpace(chi.URLParam(r, "containerID"))
		if containerID == "" {
			writeValidationFailed(w, map[string]string{
				"container_id": "Container ID is required.",
			})
			return
		}

		output, err := dockerContainerLogs(r.Context(), containerID, trimmedQuery(r, "since"))
		if err != nil {
			a.app.Logger.Error("read docker container logs failed", zap.String("container_id", containerID), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"output": output})
	})

	r.Method(stdhttp.MethodGet, "/docker/containers", containersHandler)
	r.Method(stdhttp.MethodHead, "/docker/containers", containersHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers", createContainerHandler)
	r.Method(stdhttp.MethodDelete, "/docker/containers/{containerID}", deleteContainerHandler)
	r.Method(stdhttp.MethodGet, "/docker/containers/{containerID}/logs", containerLogsHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/start", registerContainerAction("start", startDockerContainer))
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/stop", registerContainerAction("stop", stopDockerContainer))
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/restart", registerContainerAction("restart", restartDockerContainer))
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/recreate", recreateContainerHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/save-image", saveContainerImageHandler)
	r.Method(stdhttp.MethodGet, "/docker/containers/{containerID}/snapshot", snapshotContainerHandler)
	r.Method(stdhttp.MethodGet, "/docker/images", imagesHandler)
	r.Method(stdhttp.MethodHead, "/docker/images", imagesHandler)
	r.Method(stdhttp.MethodGet, "/docker/search-images", searchHandler)
	r.Method(stdhttp.MethodHead, "/docker/search-images", searchHandler)
}

func listDockerContainers(ctx context.Context) ([]dockerContainerListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerListCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "ps", "--all", "--format", "{{json .}}")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("Timed out while listing Docker containers.")
		}
		return nil, formatDockerCommandError(stderr.String(), err)
	}

	containers := make([]dockerContainerListItem, 0)
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record dockerPSRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, errors.New("Docker returned an unreadable container list.")
		}

		containers = append(containers, dockerContainerListItem{
			ID:     strings.TrimSpace(record.ID),
			Name:   strings.TrimSpace(record.Names),
			Image:  strings.TrimSpace(record.Image),
			Status: strings.TrimSpace(record.Status),
			State:  dockerContainerState(record.Status),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("Docker container output could not be read.")
	}

	sort.Slice(containers, func(i, j int) bool {
		return strings.ToLower(containers[i].Name) < strings.ToLower(containers[j].Name)
	})

	return containers, nil
}

func listDockerImages(ctx context.Context) ([]dockerImageListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerListCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "image", "ls", "--format", "{{json .}}")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("Timed out while listing Docker images.")
		}
		return nil, formatDockerCommandError(stderr.String(), err)
	}

	images := make([]dockerImageListItem, 0)
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record dockerImageLSRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, errors.New("Docker returned an unreadable image list.")
		}

		images = append(images, dockerImageListItem{
			ID:           strings.TrimSpace(record.ID),
			Repository:   strings.TrimSpace(record.Repository),
			Tag:          strings.TrimSpace(record.Tag),
			Size:         strings.TrimSpace(record.Size),
			CreatedSince: strings.TrimSpace(record.CreatedSince),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("Docker image output could not be read.")
	}

	sort.Slice(images, func(i, j int) bool {
		leftRepository := strings.ToLower(images[i].Repository)
		rightRepository := strings.ToLower(images[j].Repository)
		if leftRepository != rightRepository {
			return leftRepository < rightRepository
		}
		return strings.ToLower(images[i].Tag) < strings.ToLower(images[j].Tag)
	})

	return images, nil
}

func dockerContainerLogs(ctx context.Context, containerID, since string) (string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerLogsCommandTimeout)
	defer cancel()

	args := []string{"logs", "--tail", strconv.Itoa(dockerLogsTailLines)}
	if strings.TrimSpace(since) != "" {
		args = append(args, "--since", strings.TrimSpace(since))
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(commandCtx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("Timed out while reading Docker container logs.")
		}
		return "", formatDockerCommandError(stderr.String(), err)
	}

	return stdout.String(), nil
}

func searchDockerHubImages(ctx context.Context, query string, limit int) ([]dockerHubSearchImage, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerSearchCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "search", "--limit", strconv.Itoa(limit), "--format", "{{json .}}", query)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("Timed out while searching Docker Hub.")
		}
		return nil, formatDockerCommandError(stderr.String(), err)
	}

	results := make([]dockerHubSearchImage, 0)
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, errors.New("Docker returned an unreadable Docker Hub search result.")
		}

		name := strings.TrimSpace(stringFromAny(record["Name"]))
		if name == "" {
			continue
		}

		results = append(results, dockerHubSearchImage{
			Name:        name,
			Description: strings.TrimSpace(stringFromAny(record["Description"])),
			StarCount:   intFromAny(record["StarCount"]),
			IsOfficial:  dockerSearchOfficialValue(record["IsOfficial"]),
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("Docker Hub search output could not be read.")
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StarCount != results[j].StarCount {
			return results[i].StarCount > results[j].StarCount
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	return results, nil
}

func createDockerContainer(ctx context.Context, image string) (dockerContainerListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerContainerListItem{}, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerCreateCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "create", "--pull", "missing", "-q", image)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return dockerContainerListItem{}, errors.New("Timed out while creating the Docker container.")
		}
		return dockerContainerListItem{}, formatDockerCommandError(stderr.String(), err)
	}

	containerID := strings.TrimSpace(stdout.String())
	if containerID == "" {
		return dockerContainerListItem{}, errors.New("Docker did not return the new container identifier.")
	}

	container, err := inspectDockerContainer(commandCtx, containerID)
	if err == nil {
		return container, nil
	}

	return dockerContainerListItem{
		ID:     containerID,
		Name:   shortDockerID(containerID),
		Image:  image,
		Status: "Created",
		State:  "created",
	}, nil
}

func startDockerContainer(ctx context.Context, containerID string) (dockerContainerListItem, error) {
	return runDockerContainerAction(ctx, containerID, "start")
}

func stopDockerContainer(ctx context.Context, containerID string) (dockerContainerListItem, error) {
	return runDockerContainerAction(ctx, containerID, "stop")
}

func restartDockerContainer(ctx context.Context, containerID string) (dockerContainerListItem, error) {
	return runDockerContainerAction(ctx, containerID, "restart")
}

func deleteDockerContainer(ctx context.Context, containerID string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerActionCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "rm", "--force", containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return errors.New("Timed out while deleting the Docker container.")
		}
		return formatDockerCommandError(stderr.String(), err)
	}

	return nil
}

func recreateDockerContainer(ctx context.Context, containerID string) (dockerContainerListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerContainerListItem{}, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerCreateCommandTimeout)
	defer cancel()

	record, err := inspectDockerContainerConfig(commandCtx, containerID)
	if err != nil {
		return dockerContainerListItem{}, err
	}

	args := dockerCreateArgs(record)
	if err := deleteDockerContainer(commandCtx, containerID); err != nil {
		return dockerContainerListItem{}, err
	}

	cmd := exec.CommandContext(commandCtx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return dockerContainerListItem{}, errors.New("Timed out while recreating the Docker container.")
		}
		return dockerContainerListItem{}, formatDockerCommandError(stderr.String(), err)
	}

	newContainerID := strings.TrimSpace(stdout.String())
	if newContainerID == "" {
		return dockerContainerListItem{}, errors.New("Docker did not return the recreated container identifier.")
	}

	if record.State.Running {
		if _, err := runDockerContainerAction(commandCtx, newContainerID, "start"); err != nil {
			return dockerContainerListItem{}, err
		}
	}

	return inspectDockerContainer(commandCtx, newContainerID)
}

func saveDockerContainerImage(ctx context.Context, containerID, image string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerCreateCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "commit", containerID, image)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return errors.New("Timed out while saving the Docker container as an image.")
		}
		return formatDockerCommandError(stderr.String(), err)
	}

	return nil
}

func runDockerContainerAction(ctx context.Context, containerID, action string) (dockerContainerListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerContainerListItem{}, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerActionCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", action, containerID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return dockerContainerListItem{}, fmt.Errorf("Timed out while %s the Docker container.", dockerContainerActionPresentParticiple(action))
		}
		return dockerContainerListItem{}, formatDockerCommandError(stderr.String(), err)
	}

	return inspectDockerContainer(commandCtx, containerID)
}

func inspectDockerContainer(ctx context.Context, containerID string) (dockerContainerListItem, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "--all", "--filter", "id="+containerID, "--format", "{{json .}}")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return dockerContainerListItem{}, formatDockerCommandError(stderr.String(), err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record dockerPSRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return dockerContainerListItem{}, errors.New("Docker returned unreadable container details.")
		}

		return dockerContainerListItem{
			ID:     strings.TrimSpace(record.ID),
			Name:   strings.TrimSpace(record.Names),
			Image:  strings.TrimSpace(record.Image),
			Status: strings.TrimSpace(record.Status),
			State:  dockerContainerState(record.Status),
		}, nil
	}
	if err := scanner.Err(); err != nil {
		return dockerContainerListItem{}, errors.New("Docker container details could not be read.")
	}

	return dockerContainerListItem{}, errors.New("Docker container details were unavailable.")
}

func inspectDockerContainerConfig(ctx context.Context, containerID string) (dockerInspectRecord, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--type", "container", containerID)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return dockerInspectRecord{}, formatDockerCommandError(stderr.String(), err)
	}

	var records []dockerInspectRecord
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		return dockerInspectRecord{}, errors.New("Docker returned unreadable container details.")
	}
	if len(records) == 0 {
		return dockerInspectRecord{}, errors.New("Docker container details were unavailable.")
	}

	return records[0], nil
}

func dockerCreateArgs(record dockerInspectRecord) []string {
	args := []string{"create", "-q"}

	name := strings.TrimPrefix(strings.TrimSpace(record.Name), "/")
	if name != "" {
		args = append(args, "--name", name)
	}
	if record.Config.Hostname != "" {
		args = append(args, "--hostname", record.Config.Hostname)
	}
	if record.Config.Domainname != "" {
		args = append(args, "--domainname", record.Config.Domainname)
	}
	if record.Config.WorkingDir != "" {
		args = append(args, "--workdir", record.Config.WorkingDir)
	}
	if record.Config.User != "" {
		args = append(args, "--user", record.Config.User)
	}
	if record.Config.StopSignal != "" {
		args = append(args, "--stop-signal", record.Config.StopSignal)
	}
	if record.Config.Tty {
		args = append(args, "--tty")
	}
	if record.Config.OpenStdin {
		args = append(args, "--interactive")
	}
	if record.HostConfig.AutoRemove {
		args = append(args, "--rm")
	}
	if record.HostConfig.PublishAllPorts {
		args = append(args, "--publish-all")
	}
	if record.HostConfig.ReadonlyRootfs {
		args = append(args, "--read-only")
	}
	if record.HostConfig.Privileged {
		args = append(args, "--privileged")
	}
	if record.HostConfig.Init != nil && *record.HostConfig.Init {
		args = append(args, "--init")
	}
	if record.HostConfig.ShmSize > 0 {
		args = append(args, "--shm-size", strconv.FormatInt(record.HostConfig.ShmSize, 10))
	}
	if entrypoint := dockerEntrypointValue(record.Config.Entrypoint); entrypoint != "" {
		args = append(args, "--entrypoint", entrypoint)
	}
	if restart := dockerRestartPolicyValue(record.HostConfig.RestartPolicy); restart != "" {
		args = append(args, "--restart", restart)
	}
	if networkMode := strings.TrimSpace(record.HostConfig.NetworkMode); networkMode != "" && networkMode != "default" {
		args = append(args, "--network", networkMode)
	}

	labelKeys := make([]string, 0, len(record.Config.Labels))
	for key := range record.Config.Labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		args = append(args, "--label", key+"="+record.Config.Labels[key])
	}

	for _, env := range record.Config.Env {
		env = strings.TrimSpace(env)
		if env != "" {
			args = append(args, "--env", env)
		}
	}

	for _, host := range record.HostConfig.ExtraHosts {
		host = strings.TrimSpace(host)
		if host != "" {
			args = append(args, "--add-host", host)
		}
	}

	for _, dns := range record.HostConfig.DNS {
		dns = strings.TrimSpace(dns)
		if dns != "" {
			args = append(args, "--dns", dns)
		}
	}

	for _, dnsSearch := range record.HostConfig.DNSSearch {
		dnsSearch = strings.TrimSpace(dnsSearch)
		if dnsSearch != "" {
			args = append(args, "--dns-search", dnsSearch)
		}
	}

	for _, capability := range record.HostConfig.CapAdd {
		capability = strings.TrimSpace(capability)
		if capability != "" {
			args = append(args, "--cap-add", capability)
		}
	}

	for _, capability := range record.HostConfig.CapDrop {
		capability = strings.TrimSpace(capability)
		if capability != "" {
			args = append(args, "--cap-drop", capability)
		}
	}

	portKeys := make([]string, 0, len(record.HostConfig.PortBindings))
	for key := range record.HostConfig.PortBindings {
		portKeys = append(portKeys, key)
	}
	sort.Strings(portKeys)
	for _, key := range portKeys {
		bindings := record.HostConfig.PortBindings[key]
		if len(bindings) == 0 {
			args = append(args, "--expose", key)
			continue
		}

		for _, binding := range bindings {
			publish := dockerPublishValue(key, binding)
			if publish != "" {
				args = append(args, "--publish", publish)
			}
		}
	}

	for _, bind := range record.HostConfig.Binds {
		bind = strings.TrimSpace(bind)
		if bind != "" {
			args = append(args, "--volume", bind)
		}
	}

	bindDestinations := make(map[string]struct{}, len(record.HostConfig.Binds))
	for _, bind := range record.HostConfig.Binds {
		if destination := dockerBindDestination(bind); destination != "" {
			bindDestinations[destination] = struct{}{}
		}
	}

	for _, mount := range record.Mounts {
		if strings.TrimSpace(mount.Destination) == "" || mount.Type != "volume" {
			continue
		}
		if _, exists := bindDestinations[mount.Destination]; exists {
			continue
		}

		spec := "type=volume"
		if mount.Name != "" {
			spec += ",src=" + mount.Name
		}
		spec += ",dst=" + mount.Destination
		if !mount.RW {
			spec += ",readonly"
		}
		args = append(args, "--mount", spec)
	}

	tmpfsKeys := make([]string, 0, len(record.HostConfig.Tmpfs))
	for key := range record.HostConfig.Tmpfs {
		tmpfsKeys = append(tmpfsKeys, key)
	}
	sort.Strings(tmpfsKeys)
	for _, key := range tmpfsKeys {
		spec := key
		if value := strings.TrimSpace(record.HostConfig.Tmpfs[key]); value != "" {
			spec += ":" + value
		}
		args = append(args, "--tmpfs", spec)
	}

	exposedPortKeys := make([]string, 0, len(record.Config.ExposedPorts))
	for key := range record.Config.ExposedPorts {
		exposedPortKeys = append(exposedPortKeys, key)
	}
	sort.Strings(exposedPortKeys)
	for _, key := range exposedPortKeys {
		if _, published := record.HostConfig.PortBindings[key]; published {
			continue
		}
		args = append(args, "--expose", key)
	}

	image := strings.TrimSpace(record.Config.Image)
	if image == "" {
		image = strings.TrimSpace(record.ID)
	}
	args = append(args, image)
	args = append(args, record.Config.Cmd...)

	return args
}

func formatDockerCommandError(stderr string, fallback error) error {
	message := strings.TrimSpace(stderr)
	if message == "" && fallback != nil {
		message = strings.TrimSpace(fallback.Error())
	}
	if message == "" {
		return errors.New("Docker containers are unavailable right now.")
	}

	lowerMessage := strings.ToLower(message)
	switch {
	case strings.Contains(lowerMessage, "cannot connect to the docker daemon"),
		strings.Contains(lowerMessage, "is the docker daemon running"),
		strings.Contains(lowerMessage, "error during connect"):
		return errors.New("Docker is installed, but the daemon is unavailable right now.")
	case strings.Contains(lowerMessage, "no such container"):
		return errors.New("Docker container was not found.")
	case strings.Contains(lowerMessage, "permission denied"):
		return errors.New("FlowPanel does not have permission to access the Docker daemon.")
	case strings.Contains(lowerMessage, "toomanyrequests"),
		strings.Contains(lowerMessage, "pull rate limit"),
		strings.Contains(lowerMessage, "rate limit exceeded"):
		return errors.New("Docker Hub rate limits blocked this request. Sign in with Docker or try again later.")
	default:
		return errors.New(message)
	}
}

func dockerContainerState(status string) string {
	lowerStatus := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.HasPrefix(lowerStatus, "up "):
		return "running"
	case strings.HasPrefix(lowerStatus, "restarting"):
		return "restarting"
	case strings.HasPrefix(lowerStatus, "paused"):
		return "paused"
	case strings.HasPrefix(lowerStatus, "created"):
		return "created"
	case strings.HasPrefix(lowerStatus, "exited"):
		return "exited"
	case strings.HasPrefix(lowerStatus, "dead"):
		return "dead"
	default:
		return "unknown"
	}
}

func dockerSearchOfficialValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "ok" || normalized == "[ok]" || normalized == "true" || normalized == "yes"
	default:
		return false
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		number, err := typed.Int64()
		if err == nil {
			return int(number)
		}
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return number
		}
	}

	return 0
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func shortDockerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func dockerContainerActionPastTense(action string) string {
	switch strings.TrimSpace(action) {
	case "start":
		return "Started"
	case "stop":
		return "Stopped"
	case "restart":
		return "Restarted"
	default:
		return "Updated"
	}
}

func dockerContainerActionPresentParticiple(action string) string {
	switch strings.TrimSpace(action) {
	case "start":
		return "starting"
	case "stop":
		return "stopping"
	case "restart":
		return "restarting"
	default:
		return "updating"
	}
}

func dockerRestartPolicyValue(policy dockerRestartPolicy) string {
	name := strings.TrimSpace(policy.Name)
	if name == "" || name == "no" {
		return ""
	}
	if name == "on-failure" && policy.MaximumRetryCount > 0 {
		return fmt.Sprintf("%s:%d", name, policy.MaximumRetryCount)
	}
	return name
}

func dockerEntrypointValue(entrypoint []string) string {
	trimmed := make([]string, 0, len(entrypoint))
	for _, value := range entrypoint {
		value = strings.TrimSpace(value)
		if value != "" {
			trimmed = append(trimmed, value)
		}
	}
	if len(trimmed) == 0 {
		return ""
	}
	return strings.Join(trimmed, " ")
}

func dockerPublishValue(containerPort string, binding dockerPortBinding) string {
	hostPort := strings.TrimSpace(binding.HostPort)
	hostIP := strings.TrimSpace(binding.HostIP)
	switch {
	case hostIP != "" && hostPort != "":
		return hostIP + ":" + hostPort + ":" + containerPort
	case hostPort != "":
		return hostPort + ":" + containerPort
	case hostIP != "":
		return hostIP + "::" + containerPort
	default:
		return containerPort
	}
}

func dockerBindDestination(bind string) string {
	parts := strings.Split(bind, ":")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func dockerSnapshotFileName(container dockerContainerListItem) string {
	label := strings.TrimSpace(container.Name)
	if label == "" {
		label = shortDockerID(container.ID)
	}

	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		" ", "-",
	)
	label = replacer.Replace(label)
	label = strings.Trim(label, "-.")
	if label == "" {
		label = "docker-container"
	}

	return label + "-snapshot.tar"
}
