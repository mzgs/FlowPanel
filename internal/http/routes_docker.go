package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	dockerSearchCommandTimeout = 10 * time.Second
	dockerCreateCommandTimeout = 2 * time.Minute
	dockerSearchResultLimit    = 100
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

	r.Method(stdhttp.MethodGet, "/docker/containers", containersHandler)
	r.Method(stdhttp.MethodHead, "/docker/containers", containersHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers", createContainerHandler)
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
