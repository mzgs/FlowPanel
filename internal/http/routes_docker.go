package httpx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flowpanel/internal/config"
	"flowpanel/internal/dockercontainer"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	dockerPullCommandTimeout   = 5 * time.Minute
	dockerExportCommandTimeout = 2 * time.Minute
	dockerSearchResultLimit    = 100
	dockerSearchCandidatePages = 2
	dockerLogsTailLines        = 200
	dockerAutomaticHostPort    = 32770
)

var dockerContainerCreateMu sync.Mutex

type dockerContainerListItem struct {
	ID     string                       `json:"id"`
	Name   string                       `json:"name"`
	Image  string                       `json:"image"`
	Status string                       `json:"status"`
	State  string                       `json:"state"`
	Ports  []dockerContainerPortMapping `json:"ports"`
}

type dockerPSRecord struct {
	ID     string `json:"ID"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
	Names  string `json:"Names"`
	Ports  string `json:"Ports"`
}

type dockerContainerDetails struct {
	CPUPerc          *float64                     `json:"cpu_percent,omitempty"`
	MemoryUsageBytes *int64                       `json:"memory_usage_bytes,omitempty"`
	MemoryLimitBytes *int64                       `json:"memory_limit_bytes,omitempty"`
	MemoryPerc       *float64                     `json:"memory_percent,omitempty"`
	Ports            []dockerContainerPortMapping `json:"ports"`
}

type dockerContainerPortMapping struct {
	ContainerPort string `json:"container_port"`
	HostIP        string `json:"host_ip"`
	HostPort      string `json:"host_port"`
	Public        bool   `json:"public"`
}

type dockerImageListItem struct {
	ID           string `json:"id"`
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
	Reference    string `json:"reference"`
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

type renameDockerContainerRequest struct {
	Name string `json:"name"`
}

type pullDockerImageRequest struct {
	Image string `json:"image"`
}

type deleteDockerImageRequest struct {
	Image string `json:"image"`
}

type saveDockerContainerImageRequest struct {
	Image string `json:"image"`
}

type dockerContainerSettings struct {
	Ports                []dockerContainerPortMapping `json:"ports"`
	Environment          []dockerEnvironmentVariable  `json:"environment"`
	Volumes              []dockerVolumeMapping        `json:"volumes"`
	PublishAllPorts      bool                         `json:"publish_all_ports"`
	VolumeSourceBasePath string                       `json:"volume_source_base_path,omitempty"`
}

type dockerContainerSettingsResponse struct {
	Settings dockerContainerSettings `json:"settings"`
}

type updateDockerContainerSettingsRequest struct {
	Ports       []dockerContainerPortMapping `json:"ports"`
	Environment *[]dockerEnvironmentVariable `json:"environment"`
	Volumes     *[]dockerVolumeMapping       `json:"volumes"`
}

type dockerEnvironmentVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type dockerVolumeMapping struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only"`
}

type dockerInspectRecord = dockercontainer.Record
type dockerInspectMount = dockercontainer.Mount
type dockerPortBinding = dockercontainer.PortBinding

type dockerStatsRecord struct {
	CPUPerc  string `json:"CPUPerc"`
	MemPerc  string `json:"MemPerc"`
	MemUsage string `json:"MemUsage"`
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

	pullImageHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		var input pullDockerImageRequest
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
		if err := pullDockerImage(actionCtx, input.Image); err != nil {
			a.app.Logger.Error("pull docker image failed", zap.String("image", input.Image), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "pull", "docker_image", input.Image, input.Image, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"pull",
			"docker_image",
			input.Image,
			input.Image,
			"succeeded",
			fmt.Sprintf("Pulled Docker image %q.", input.Image),
		)
		w.WriteHeader(stdhttp.StatusNoContent)
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
			fmt.Sprintf("Created and started Docker container %q from %q.", label, input.Image),
		)
		if err := a.reconcileFirewall(actionCtx); err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "container created but firewall reconciliation failed", "container": container})
			return
		}
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"container": container})
	})

	deleteImageHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Docker == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "docker runtime is not configured"})
			return
		}

		var input deleteDockerImageRequest
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
		if err := deleteDockerImage(actionCtx, input.Image); err != nil {
			a.app.Logger.Error("delete docker image failed", zap.String("image", input.Image), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "delete", "docker_image", input.Image, input.Image, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"delete",
			"docker_image",
			input.Image,
			input.Image,
			"succeeded",
			fmt.Sprintf("Deleted Docker image %q.", input.Image),
		)
		w.WriteHeader(stdhttp.StatusNoContent)
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
			if err := a.reconcileFirewall(actionCtx); err != nil {
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "container changed but firewall reconciliation failed", "container": container})
				return
			}
			writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
		}
	}

	renameContainerHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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

		var input renameDockerContainerRequest
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" {
			writeValidationFailed(w, map[string]string{
				"name": "Container name is required.",
			})
			return
		}

		actionCtx := backgroundRequestContext(r.Context())
		previousLabel := shortDockerID(containerID)
		if container, err := inspectDockerContainer(actionCtx, containerID); err == nil {
			if label := strings.TrimSpace(container.Name); label != "" {
				previousLabel = label
			}
			if strings.TrimSpace(container.Name) == input.Name {
				writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
				return
			}
		}

		container, err := renameDockerContainer(actionCtx, containerID, input.Name)
		if err != nil {
			a.app.Logger.Error(
				"rename docker container failed",
				zap.String("container_id", containerID),
				zap.String("name", input.Name),
				zap.Error(err),
			)
			a.mutationEvent(actionCtx, "runtime", "rename", "docker_container", containerID, previousLabel, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		label := strings.TrimSpace(container.Name)
		if label == "" {
			label = input.Name
		}
		resourceID := container.ID
		if resourceID == "" {
			resourceID = containerID
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"rename",
			"docker_container",
			resourceID,
			label,
			"succeeded",
			fmt.Sprintf("Renamed Docker container %q to %q.", previousLabel, label),
		)
		writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
	})

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
		deleteData := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("delete_data")), "true")
		var dataPaths []string
		if deleteData {
			record, err := inspectDockerContainerConfig(actionCtx, containerID)
			if err != nil {
				a.app.Logger.Error("inspect docker container data before delete failed", zap.String("container_id", containerID), zap.Error(err))
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "Container data paths could not be verified, so nothing was deleted."})
				return
			}
			dataPaths = dockerManagedDataPaths(record)
		}

		if err := deleteDockerContainer(actionCtx, containerID); err != nil {
			a.app.Logger.Error("delete docker container failed", zap.String("container_id", containerID), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "delete", "docker_container", containerID, label, "failed", err.Error())
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}
		for _, dataPath := range dataPaths {
			if err := os.RemoveAll(dataPath); err != nil {
				a.app.Logger.Error("delete docker container data failed", zap.String("container_id", containerID), zap.String("path", dataPath), zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", "delete", "docker_container", containerID, label, "failed", "Container deleted but its managed data could not be removed.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "Container deleted but its managed data could not be removed."})
				return
			}
		}
		activityMessage := fmt.Sprintf("Deleted Docker container %q.", label)
		if deleteData {
			activityMessage = fmt.Sprintf("Deleted Docker container %q and its managed data.", label)
		}

		a.mutationEvent(
			actionCtx,
			"runtime",
			"delete",
			"docker_container",
			containerID,
			label,
			"succeeded",
			activityMessage,
		)
		if err := a.reconcileFirewall(actionCtx); err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "container deleted but firewall reconciliation failed"})
			return
		}
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
		if err := a.reconcileFirewall(actionCtx); err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "container recreated but firewall reconciliation failed", "container": container})
			return
		}
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

	containerDetailsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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

		details, err := inspectDockerContainerDetails(r.Context(), containerID)
		if err != nil {
			a.app.Logger.Error("inspect docker container details failed", zap.String("container_id", containerID), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"details": details})
	})

	containerSettingsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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

		record, err := inspectDockerContainerConfig(r.Context(), containerID)
		if err != nil {
			a.app.Logger.Error("inspect docker container settings failed", zap.String("container_id", containerID), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, dockerContainerSettingsResponse{
			Settings: dockerContainerSettingsFromRecord(record),
		})
	})

	updateContainerSettingsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
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

		var input updateDockerContainerSettingsRequest
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		actionCtx := backgroundRequestContext(r.Context())
		commandCtx, cancel := context.WithTimeout(actionCtx, dockerCreateCommandTimeout)
		defer cancel()

		record, err := inspectDockerContainerConfig(commandCtx, containerID)
		if err != nil {
			a.app.Logger.Error("inspect docker container settings before update failed", zap.String("container_id", containerID), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}

		fieldErrors := applyDockerContainerPorts(&record, input.Ports)
		if input.Environment != nil {
			fieldErrors = mergeDockerValidationErrors(
				fieldErrors,
				applyDockerContainerEnvironment(&record, *input.Environment),
			)
		}
		if input.Volumes != nil {
			fieldErrors = mergeDockerValidationErrors(
				fieldErrors,
				applyDockerContainerVolumes(&record, *input.Volumes),
			)
		}
		if len(fieldErrors) > 0 {
			writeValidationFailed(w, fieldErrors)
			return
		}

		container, err := recreateDockerContainerWithConfig(commandCtx, containerID, record)
		if err != nil {
			a.app.Logger.Error("update docker container settings failed", zap.String("container_id", containerID), zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", "update", "docker_container", containerID, shortDockerID(containerID), "failed", err.Error())
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
			"update",
			"docker_container",
			container.ID,
			label,
			"succeeded",
			fmt.Sprintf("Updated Docker container %q settings.", label),
		)
		if err := a.reconcileFirewall(actionCtx); err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "container updated but firewall reconciliation failed", "container": container})
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"container": container})
	})

	r.Method(stdhttp.MethodGet, "/docker/containers", containersHandler)
	r.Method(stdhttp.MethodHead, "/docker/containers", containersHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers", createContainerHandler)
	r.Method(stdhttp.MethodDelete, "/docker/containers/{containerID}", deleteContainerHandler)
	r.Method(stdhttp.MethodGet, "/docker/containers/{containerID}/settings", containerSettingsHandler)
	r.Method(stdhttp.MethodHead, "/docker/containers/{containerID}/settings", containerSettingsHandler)
	r.Method(stdhttp.MethodPut, "/docker/containers/{containerID}/settings", updateContainerSettingsHandler)
	r.Method(stdhttp.MethodGet, "/docker/containers/{containerID}/details", containerDetailsHandler)
	r.Method(stdhttp.MethodGet, "/docker/containers/{containerID}/logs", containerLogsHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/start", registerContainerAction("start", startDockerContainer))
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/stop", registerContainerAction("stop", stopDockerContainer))
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/restart", registerContainerAction("restart", restartDockerContainer))
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/rename", renameContainerHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/recreate", recreateContainerHandler)
	r.Method(stdhttp.MethodPost, "/docker/containers/{containerID}/save-image", saveContainerImageHandler)
	r.Method(stdhttp.MethodGet, "/docker/containers/{containerID}/snapshot", snapshotContainerHandler)
	r.Method(stdhttp.MethodGet, "/docker/images", imagesHandler)
	r.Method(stdhttp.MethodHead, "/docker/images", imagesHandler)
	r.Method(stdhttp.MethodPost, "/docker/images/pull", pullImageHandler)
	r.Method(stdhttp.MethodPost, "/docker/images/delete", deleteImageHandler)
	r.Method(stdhttp.MethodGet, "/docker/search-images", searchHandler)
	r.Method(stdhttp.MethodHead, "/docker/search-images", searchHandler)
}

func listDockerContainers(ctx context.Context) ([]dockerContainerListItem, error) {
	return listDockerContainersByArgs(ctx, "ps", "--all", "--format", "{{json .}}")
}

func listDockerContainersByAncestor(ctx context.Context, image string) ([]dockerContainerListItem, error) {
	return listDockerContainersByArgs(ctx, "ps", "--all", "--filter", "ancestor="+image, "--format", "{{json .}}")
}

func listDockerContainersByArgs(ctx context.Context, args ...string) ([]dockerContainerListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerListCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", args...)
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
			Ports:  dockerPSPortMappings(record.Ports),
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
			Reference:    dockerImageReference(strings.TrimSpace(record.Repository), strings.TrimSpace(record.Tag), strings.TrimSpace(record.ID)),
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

func inspectDockerContainerDetails(ctx context.Context, containerID string) (dockerContainerDetails, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerContainerDetails{}, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerListCommandTimeout)
	defer cancel()

	record, err := inspectDockerContainerConfig(commandCtx, containerID)
	if err != nil {
		return dockerContainerDetails{}, err
	}

	details := dockerContainerDetails{
		Ports: dockerContainerPortMappings(record),
	}

	if !record.State.Running {
		return details, nil
	}

	stats, err := dockerContainerStats(commandCtx, containerID)
	if err != nil {
		return dockerContainerDetails{}, err
	}

	details.CPUPerc = parseDockerStatsPercent(stats.CPUPerc)
	details.MemoryPerc = parseDockerStatsPercent(stats.MemPerc)
	details.MemoryUsageBytes, details.MemoryLimitBytes = parseDockerStatsMemoryUsage(stats.MemUsage)
	return details, nil
}

func dockerContainerSettingsFromRecord(record dockerInspectRecord) dockerContainerSettings {
	volumeSourceBasePath := ""
	if path, err := dockerAutomaticVolumeBasePath(dockerAutomaticVolumeContainerName(record)); err == nil {
		volumeSourceBasePath = path
	}

	return dockerContainerSettings{
		Ports:                dockerContainerPortMappings(record),
		Environment:          dockerContainerEnvironment(record),
		Volumes:              dockerContainerVolumeMappings(record),
		PublishAllPorts:      record.HostConfig.PublishAllPorts,
		VolumeSourceBasePath: volumeSourceBasePath,
	}
}

func dockerContainerEnvironment(record dockerInspectRecord) []dockerEnvironmentVariable {
	if len(record.Config.Env) == 0 {
		return nil
	}

	values := make([]dockerEnvironmentVariable, 0, len(record.Config.Env))
	for _, entry := range record.Config.Env {
		if strings.TrimSpace(entry) == "" {
			continue
		}

		key, value, hasValue := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !hasValue {
			value = ""
		}
		values = append(values, dockerEnvironmentVariable{
			Key:   key,
			Value: value,
		})
	}

	if len(values) == 0 {
		return nil
	}

	return values
}

func dockerContainerVolumeMappings(record dockerInspectRecord) []dockerVolumeMapping {
	values := make([]dockerVolumeMapping, 0, len(record.HostConfig.Binds)+len(record.Mounts))
	seenDestinations := make(map[string]struct{}, len(record.HostConfig.Binds)+len(record.Mounts))

	for _, bind := range record.HostConfig.Binds {
		mapping, ok := parseDockerVolumeSpec(bind)
		if !ok {
			continue
		}
		if _, exists := seenDestinations[mapping.Destination]; exists {
			continue
		}
		seenDestinations[mapping.Destination] = struct{}{}
		values = append(values, mapping)
	}

	for _, mount := range record.Mounts {
		mapping, ok := dockerVolumeMappingFromMount(mount)
		if !ok {
			continue
		}
		if _, exists := seenDestinations[mapping.Destination]; exists {
			continue
		}
		seenDestinations[mapping.Destination] = struct{}{}
		values = append(values, mapping)
	}

	if len(values) == 0 {
		return nil
	}
	return values
}

func dockerAutomaticPortMappings(ctx context.Context, record dockerInspectRecord) ([]dockerContainerPortMapping, error) {
	keys := make([]string, 0, len(record.Config.ExposedPorts))
	for key := range record.Config.ExposedPorts {
		if port := strings.TrimSpace(strings.SplitN(key, "/", 2)[0]); validDockerPortNumber(port) {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return nil, nil
	}

	records, err := dockercontainer.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect existing Docker ports: %w", err)
	}
	used := make(map[int]struct{})
	for _, existing := range records {
		for _, port := range dockerContainerPortMappings(existing) {
			if hostPort, err := strconv.Atoi(port.HostPort); err == nil {
				used[hostPort] = struct{}{}
			}
		}
	}

	ports := make([]dockerContainerPortMapping, 0, len(keys))
	hostPort := dockerAutomaticHostPort
	for _, key := range keys {
		for ; hostPort <= 65535; hostPort++ {
			if _, exists := used[hostPort]; !exists {
				break
			}
		}
		if hostPort > 65535 {
			return nil, errors.New("No automatic Docker host ports are available.")
		}
		ports = append(ports, dockerContainerPortMapping{
			ContainerPort: key,
			HostPort:      strconv.Itoa(hostPort),
			Public:        true,
		})
		used[hostPort] = struct{}{}
		hostPort++
	}
	return ports, nil
}

func dockerAutomaticVolumeMappings(record dockerInspectRecord) ([]dockerVolumeMapping, error) {
	if len(record.Config.Volumes) == 0 {
		return nil, nil
	}

	containerName := dockerAutomaticVolumeContainerName(record)
	if containerName == "" {
		return nil, errors.New("Docker container name is unavailable for automatic volume storage.")
	}

	destinations := make([]string, 0, len(record.Config.Volumes))
	for destination := range record.Config.Volumes {
		destination = strings.TrimSpace(destination)
		if destination != "" {
			destinations = append(destinations, destination)
		}
	}
	sort.Strings(destinations)

	values := make([]dockerVolumeMapping, 0, len(destinations))
	for _, destination := range destinations {
		source, err := dockerAutomaticVolumePath(containerName, destination)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(source, 0o755); err != nil {
			return nil, fmt.Errorf("create automatic Docker volume path %q: %w", source, err)
		}
		values = append(values, dockerVolumeMapping{
			Source:      source,
			Destination: destination,
		})
	}

	if len(values) == 0 {
		return nil, nil
	}
	return values, nil
}

func dockerAutomaticVolumeContainerName(record dockerInspectRecord) string {
	containerName := strings.TrimPrefix(strings.TrimSpace(record.Name), "/")
	if containerName == "" {
		containerName = shortDockerID(record.ID)
	}
	return containerName
}

func dockerAutomaticVolumeBasePath(containerName string) (string, error) {
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return "", errors.New("Docker container name is required for automatic volume storage.")
	}

	return filepath.Join(config.FlowPanelDataPath(), "docker_volumes", containerName), nil
}

func dockerManagedDataPaths(record dockerInspectRecord) []string {
	root := filepath.Clean(filepath.Join(config.FlowPanelDataPath(), "docker_volumes"))
	paths := make([]string, 0, 1)
	seen := make(map[string]struct{})
	for _, volume := range dockerContainerVolumeMappings(record) {
		source := filepath.Clean(strings.TrimSpace(volume.Source))
		relative, err := filepath.Rel(root, source)
		if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		containerDir := strings.SplitN(relative, string(filepath.Separator), 2)[0]
		path := filepath.Join(root, containerDir)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func dockerAutomaticVolumePath(containerName, destination string) (string, error) {
	basePath, err := dockerAutomaticVolumeBasePath(containerName)
	if err != nil {
		return "", err
	}

	relative := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(strings.TrimSpace(destination), "/")))
	switch {
	case relative == ".":
		relative = "root"
	case relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)):
		return "", fmt.Errorf("Docker volume destination %q is invalid for automatic storage", destination)
	}

	return filepath.Join(basePath, relative), nil
}

func dockerVolumeMappingFromMount(mount dockerInspectMount) (dockerVolumeMapping, bool) {
	destination := strings.TrimSpace(mount.Destination)
	if destination == "" {
		return dockerVolumeMapping{}, false
	}

	switch mount.Type {
	case "bind":
		source := strings.TrimSpace(mount.Source)
		if source == "" {
			return dockerVolumeMapping{}, false
		}
		return dockerVolumeMapping{
			Source:      source,
			Destination: destination,
			ReadOnly:    !mount.RW,
		}, true
	case "volume":
		source := strings.TrimSpace(mount.Name)
		if source == "" {
			source = strings.TrimSpace(mount.Source)
		}
		if source == "" {
			return dockerVolumeMapping{}, false
		}
		return dockerVolumeMapping{
			Source:      source,
			Destination: destination,
			ReadOnly:    !mount.RW,
		}, true
	default:
		return dockerVolumeMapping{}, false
	}
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

	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		seen[strings.ToLower(result.Name)] = struct{}{}
	}
	for page := 2; page <= dockerSearchCandidatePages; page++ {
		pageResults, err := searchDockerHubImagesPage(commandCtx, query, limit, page)
		if err != nil {
			break
		}
		for _, result := range pageResults {
			key := strings.ToLower(result.Name)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			results = append(results, result)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].StarCount != results[j].StarCount {
			return results[i].StarCount > results[j].StarCount
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func searchDockerHubImagesPage(ctx context.Context, query string, pageSize, page int) ([]dockerHubSearchImage, error) {
	requestURL := fmt.Sprintf(
		"https://hub.docker.com/v2/search/repositories/?query=%s&page_size=%d&page=%d",
		url.QueryEscape(query),
		pageSize,
		page,
	)
	request, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "FlowPanel")

	response, err := stdhttp.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != stdhttp.StatusOK {
		return nil, fmt.Errorf("Docker Hub search returned %s", response.Status)
	}

	var payload struct {
		Results []struct {
			Name        string `json:"repo_name"`
			Description string `json:"short_description"`
			StarCount   int    `json:"star_count"`
			IsOfficial  bool   `json:"is_official"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	results := make([]dockerHubSearchImage, 0, len(payload.Results))
	for _, result := range payload.Results {
		name := strings.TrimSpace(result.Name)
		if name == "" {
			continue
		}
		results = append(results, dockerHubSearchImage{
			Name:        name,
			Description: strings.TrimSpace(result.Description),
			StarCount:   result.StarCount,
			IsOfficial:  result.IsOfficial,
		})
	}
	return results, nil
}

func createDockerContainer(ctx context.Context, image string) (dockerContainerListItem, error) {
	dockerContainerCreateMu.Lock()
	defer dockerContainerCreateMu.Unlock()

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

	record, err := inspectDockerContainerConfig(commandCtx, containerID)
	if err == nil {
		managedPorts, portErr := dockerAutomaticPortMappings(commandCtx, record)
		if portErr != nil {
			_ = deleteDockerContainer(commandCtx, containerID)
			return dockerContainerListItem{}, portErr
		}
		managedVolumes, volumeErr := dockerAutomaticVolumeMappings(record)
		if volumeErr != nil {
			_ = deleteDockerContainer(commandCtx, containerID)
			return dockerContainerListItem{}, volumeErr
		}
		if fieldErrors := applyDockerContainerPorts(&record, managedPorts); len(fieldErrors) > 0 {
			_ = deleteDockerContainer(commandCtx, containerID)
			return dockerContainerListItem{}, errors.New("FlowPanel could not publish the declared Docker ports.")
		}
		if len(managedVolumes) > 0 {
			if fieldErrors := applyDockerContainerVolumes(&record, managedVolumes); len(fieldErrors) > 0 {
				_ = deleteDockerContainer(commandCtx, containerID)
				return dockerContainerListItem{}, errors.New("FlowPanel could not prepare automatic Docker volume storage.")
			}
		}
		if len(managedPorts) > 0 || len(managedVolumes) > 0 {
			container, recreateErr := recreateDockerContainerWithConfig(commandCtx, containerID, record)
			if recreateErr != nil {
				return dockerContainerListItem{}, recreateErr
			}
			containerID = container.ID
		}
	}

	container, err := startDockerContainer(commandCtx, containerID)
	if err != nil {
		return dockerContainerListItem{}, fmt.Errorf("Docker container was created but could not be started: %w", err)
	}
	return container, nil
}

func pullDockerImage(ctx context.Context, image string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerPullCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "pull", image)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return errors.New("Timed out while pulling the Docker image.")
		}
		return formatDockerCommandError(stderr.String(), err)
	}

	return nil
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

func renameDockerContainer(ctx context.Context, containerID, name string) (dockerContainerListItem, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerContainerListItem{}, errors.New("Docker is not installed on this server.")
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerActionCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "rename", containerID, name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return dockerContainerListItem{}, errors.New("Timed out while renaming the Docker container.")
		}
		return dockerContainerListItem{}, formatDockerCommandError(stderr.String(), err)
	}

	return inspectDockerContainer(commandCtx, containerID)
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

func deleteDockerImage(ctx context.Context, image string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker is not installed on this server.")
	}

	containers, err := listDockerContainersByAncestor(ctx, image)
	if err != nil {
		return err
	}
	if len(containers) > 0 {
		return fmt.Errorf("Remove containers using image %q first: %s.", image, dockerContainerLabelsSummary(containers, 3))
	}

	commandCtx, cancel := context.WithTimeout(ctx, dockerActionCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "docker", "image", "rm", image)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return errors.New("Timed out while deleting the Docker image.")
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

	return recreateDockerContainerWithConfig(commandCtx, containerID, record)
}

func recreateDockerContainerWithConfig(
	ctx context.Context,
	containerID string,
	record dockerInspectRecord,
) (dockerContainerListItem, error) {
	args := dockercontainer.CreateArgs(record)
	if err := deleteDockerContainer(ctx, containerID); err != nil {
		return dockerContainerListItem{}, err
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return dockerContainerListItem{}, errors.New("Timed out while recreating the Docker container.")
		}
		return dockerContainerListItem{}, formatDockerCommandError(stderr.String(), err)
	}

	newContainerID := strings.TrimSpace(stdout.String())
	if newContainerID == "" {
		return dockerContainerListItem{}, errors.New("Docker did not return the recreated container identifier.")
	}

	if record.State.Running {
		if _, err := runDockerContainerAction(ctx, newContainerID, "start"); err != nil {
			return dockerContainerListItem{}, err
		}
	}

	return inspectDockerContainer(ctx, newContainerID)
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
			Ports:  dockerPSPortMappings(record.Ports),
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

func dockerContainerStats(ctx context.Context, containerID string) (dockerStatsRecord, error) {
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", containerID)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return dockerStatsRecord{}, errors.New("Timed out while reading Docker container resource usage.")
		}
		return dockerStatsRecord{}, formatDockerCommandError(stderr.String(), err)
	}

	line := strings.TrimSpace(stdout.String())
	if line == "" {
		return dockerStatsRecord{}, errors.New("Docker container resource usage is unavailable right now.")
	}

	var record dockerStatsRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		return dockerStatsRecord{}, errors.New("Docker returned unreadable container resource details.")
	}

	return record, nil
}

func dockerContainerPortMappings(record dockerInspectRecord) []dockerContainerPortMapping {
	keys := make(map[string]struct{}, len(record.Config.ExposedPorts)+len(record.Network.Ports)+len(record.HostConfig.PortBindings))
	for key := range record.Config.ExposedPorts {
		keys[key] = struct{}{}
	}
	for key := range record.Network.Ports {
		keys[key] = struct{}{}
	}
	for key := range record.HostConfig.PortBindings {
		keys[key] = struct{}{}
	}
	if len(keys) == 0 {
		return nil
	}

	portKeys := make([]string, 0, len(keys))
	for key := range keys {
		portKeys = append(portKeys, key)
	}
	sort.Strings(portKeys)

	ports := make([]dockerContainerPortMapping, 0, len(portKeys))
	for _, key := range portKeys {
		bindings := record.Network.Ports[key]
		if len(bindings) == 0 {
			bindings = record.HostConfig.PortBindings[key]
		}
		if len(bindings) == 0 {
			ports = append(ports, dockerContainerPortMapping{ContainerPort: key})
			continue
		}

		for _, binding := range bindings {
			hostIP := strings.TrimSpace(binding.HostIP)
			ports = append(ports, dockerContainerPortMapping{
				ContainerPort: key,
				HostIP:        hostIP,
				HostPort:      strings.TrimSpace(binding.HostPort),
				Public:        dockerPortBindingPublic(hostIP),
			})
		}
	}

	return ports
}

func dockerPSPortMappings(value string) []dockerContainerPortMapping {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	ports := make([]dockerContainerPortMapping, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		hostBinding, containerPort, hasBinding := strings.Cut(entry, "->")
		if !hasBinding {
			ports = append(ports, dockerContainerPortMapping{ContainerPort: entry})
			continue
		}

		hostIP, hostPort := parseDockerPSHostBinding(hostBinding)
		ports = append(ports, dockerContainerPortMapping{
			ContainerPort: strings.TrimSpace(containerPort),
			HostIP:        hostIP,
			HostPort:      hostPort,
			Public:        hostPort != "" && dockerPortBindingPublic(hostIP),
		})
	}

	if len(ports) == 0 {
		return nil
	}

	return ports
}

func parseDockerPSHostBinding(value string) (string, string) {
	host := strings.TrimSpace(value)
	if host == "" {
		return "", ""
	}

	if strings.HasPrefix(host, "[") {
		if end := strings.LastIndex(host, "]:"); end > 0 {
			return strings.TrimSpace(host[:end+1]), strings.TrimSpace(host[end+2:])
		}
	}

	if strings.HasPrefix(host, ":::") {
		return "::", strings.TrimSpace(host[3:])
	}

	if lastColon := strings.LastIndex(host, ":"); lastColon >= 0 {
		return strings.TrimSpace(host[:lastColon]), strings.TrimSpace(host[lastColon+1:])
	}

	return "", host
}

func dockerPortBindingPublic(hostIP string) bool {
	normalized := strings.ToLower(strings.TrimSpace(hostIP))
	return normalized == "" || normalized == "0.0.0.0" || normalized == "::" || normalized == "[::]"
}

func parseDockerStatsPercent(value string) *float64 {
	normalized := strings.TrimSuffix(strings.TrimSpace(value), "%")
	if normalized == "" || normalized == "--" {
		return nil
	}

	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return nil
	}

	return &parsed
}

func parseDockerStatsMemoryUsage(value string) (*int64, *int64) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return nil, nil
	}

	used := parseDockerStatsBytes(parts[0])
	limit := parseDockerStatsBytes(parts[1])
	return used, limit
}

func parseDockerStatsBytes(value string) *int64 {
	normalized := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "B"))
	if normalized == "" || normalized == "--" {
		return nil
	}

	units := []struct {
		suffix     string
		multiplier float64
	}{
		{suffix: "ki", multiplier: 1024},
		{suffix: "mi", multiplier: 1024 * 1024},
		{suffix: "gi", multiplier: 1024 * 1024 * 1024},
		{suffix: "ti", multiplier: 1024 * 1024 * 1024 * 1024},
		{suffix: "k", multiplier: 1000},
		{suffix: "m", multiplier: 1000 * 1000},
		{suffix: "g", multiplier: 1000 * 1000 * 1000},
		{suffix: "t", multiplier: 1000 * 1000 * 1000 * 1000},
		{suffix: "b", multiplier: 1},
	}

	lower := strings.ToLower(normalized)
	for _, unit := range units {
		if !strings.HasSuffix(lower, unit.suffix) {
			continue
		}

		number := strings.TrimSpace(normalized[:len(normalized)-len(unit.suffix)])
		parsed, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return nil
		}

		bytes := int64(parsed * unit.multiplier)
		return &bytes
	}

	parsed, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return nil
	}

	bytes := int64(parsed)
	return &bytes
}

func applyDockerContainerPorts(record *dockerInspectRecord, requested []dockerContainerPortMapping) map[string]string {
	if record == nil {
		return map[string]string{
			"ports": "Container settings are unavailable.",
		}
	}

	knownPorts := dockerKnownContainerPorts(*record)
	fieldErrors := make(map[string]string)
	nextBindings := make(map[string][]dockerPortBinding)
	seen := make(map[string]struct{})

	for index, port := range requested {
		fieldName := fmt.Sprintf("ports[%d].host_port", index)
		containerPort := strings.TrimSpace(port.ContainerPort)
		hostIP := strings.TrimSpace(port.HostIP)
		hostPort := strings.TrimSpace(port.HostPort)

		if containerPort == "" {
			fieldErrors[fieldName] = "Container port is required."
			continue
		}
		if _, ok := knownPorts[containerPort]; !ok {
			fieldErrors[fieldName] = "This container port is no longer available."
			continue
		}
		if hostPort == "" {
			continue
		}
		if !validDockerPortNumber(hostPort) {
			fieldErrors[fieldName] = "Enter a port between 1 and 65535."
			continue
		}

		bindingKey := containerPort + "|" + hostIP + "|" + hostPort
		if _, exists := seen[bindingKey]; exists {
			fieldErrors[fieldName] = "This published port is duplicated."
			continue
		}

		seen[bindingKey] = struct{}{}
		nextBindings[containerPort] = append(nextBindings[containerPort], dockerPortBinding{
			HostIP:   hostIP,
			HostPort: hostPort,
		})
	}

	if len(fieldErrors) > 0 {
		return fieldErrors
	}

	if len(nextBindings) == 0 {
		record.HostConfig.PortBindings = nil
	} else {
		record.HostConfig.PortBindings = nextBindings
	}
	record.HostConfig.PublishAllPorts = false
	return nil
}

func applyDockerContainerEnvironment(record *dockerInspectRecord, requested []dockerEnvironmentVariable) map[string]string {
	if record == nil {
		return map[string]string{
			"environment": "Container settings are unavailable.",
		}
	}

	fieldErrors := make(map[string]string)
	seen := make(map[string]struct{}, len(requested))
	nextEnvironment := make([]string, 0, len(requested))

	for index, variable := range requested {
		fieldPrefix := fmt.Sprintf("environment[%d]", index)
		key := strings.TrimSpace(variable.Key)

		switch {
		case key == "":
			fieldErrors[fieldPrefix+".key"] = "Key is required."
		case strings.Contains(key, "="):
			fieldErrors[fieldPrefix+".key"] = "Key must not contain =."
		case strings.ContainsRune(key, '\x00'):
			fieldErrors[fieldPrefix+".key"] = "Key must not contain null bytes."
		case strings.ContainsAny(key, "\r\n"):
			fieldErrors[fieldPrefix+".key"] = "Key must stay on a single line."
		default:
			if _, exists := seen[key]; exists {
				fieldErrors[fieldPrefix+".key"] = "Each key must be unique."
			} else {
				seen[key] = struct{}{}
			}
		}

		switch {
		case strings.ContainsRune(variable.Value, '\x00'):
			fieldErrors[fieldPrefix+".value"] = "Value must not contain null bytes."
		case strings.ContainsAny(variable.Value, "\r\n"):
			fieldErrors[fieldPrefix+".value"] = "Value must stay on a single line."
		}

		if _, hasKeyError := fieldErrors[fieldPrefix+".key"]; hasKeyError {
			continue
		}
		if _, hasValueError := fieldErrors[fieldPrefix+".value"]; hasValueError {
			continue
		}

		nextEnvironment = append(nextEnvironment, key+"="+variable.Value)
	}

	if len(fieldErrors) > 0 {
		return fieldErrors
	}

	if len(nextEnvironment) == 0 {
		record.Config.Env = nil
	} else {
		record.Config.Env = nextEnvironment
	}

	return nil
}

func applyDockerContainerVolumes(record *dockerInspectRecord, requested []dockerVolumeMapping) map[string]string {
	if record == nil {
		return map[string]string{
			"volumes": "Container settings are unavailable.",
		}
	}

	fieldErrors := make(map[string]string)
	seenDestinations := make(map[string]struct{}, len(requested))
	nextBinds := make([]string, 0, len(requested))

	for index, volume := range requested {
		fieldPrefix := fmt.Sprintf("volumes[%d]", index)
		source := strings.TrimSpace(volume.Source)
		destination := strings.TrimSpace(volume.Destination)

		switch {
		case source == "":
			fieldErrors[fieldPrefix+".source"] = "Source is required."
		case strings.ContainsRune(source, '\x00'):
			fieldErrors[fieldPrefix+".source"] = "Source must not contain null bytes."
		case strings.ContainsAny(source, "\r\n"):
			fieldErrors[fieldPrefix+".source"] = "Source must stay on a single line."
		case strings.Contains(source, ":"):
			fieldErrors[fieldPrefix+".source"] = "Source must not contain :."
		}

		switch {
		case destination == "":
			fieldErrors[fieldPrefix+".destination"] = "Container path is required."
		case !strings.HasPrefix(destination, "/"):
			fieldErrors[fieldPrefix+".destination"] = "Container path must start with /."
		case strings.ContainsRune(destination, '\x00'):
			fieldErrors[fieldPrefix+".destination"] = "Container path must not contain null bytes."
		case strings.ContainsAny(destination, "\r\n"):
			fieldErrors[fieldPrefix+".destination"] = "Container path must stay on a single line."
		case strings.Contains(destination, ":"):
			fieldErrors[fieldPrefix+".destination"] = "Container path must not contain :."
		default:
			if _, exists := seenDestinations[destination]; exists {
				fieldErrors[fieldPrefix+".destination"] = "Each container path must be unique."
			} else {
				seenDestinations[destination] = struct{}{}
			}
		}

		if _, hasSourceError := fieldErrors[fieldPrefix+".source"]; hasSourceError {
			continue
		}
		if _, hasDestinationError := fieldErrors[fieldPrefix+".destination"]; hasDestinationError {
			continue
		}

		spec := source + ":" + destination
		if volume.ReadOnly {
			spec += ":ro"
		}
		nextBinds = append(nextBinds, spec)
	}

	if len(fieldErrors) > 0 {
		return fieldErrors
	}

	if len(nextBinds) == 0 {
		record.HostConfig.Binds = nil
	} else {
		record.HostConfig.Binds = nextBinds
	}

	if len(record.Mounts) == 0 {
		return nil
	}

	nextMounts := make([]dockerInspectMount, 0, len(record.Mounts))
	for _, mount := range record.Mounts {
		if mount.Type == "volume" || mount.Type == "bind" {
			continue
		}
		nextMounts = append(nextMounts, mount)
	}
	if len(nextMounts) == 0 {
		record.Mounts = nil
	} else {
		record.Mounts = nextMounts
	}

	return nil
}

func mergeDockerValidationErrors(groups ...map[string]string) map[string]string {
	var merged map[string]string
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		if merged == nil {
			merged = make(map[string]string, len(group))
		}
		for key, value := range group {
			merged[key] = value
		}
	}
	return merged
}

func dockerKnownContainerPorts(record dockerInspectRecord) map[string]struct{} {
	ports := make(map[string]struct{}, len(record.Config.ExposedPorts)+len(record.HostConfig.PortBindings))
	for key := range record.Config.ExposedPorts {
		key = strings.TrimSpace(key)
		if key != "" {
			ports[key] = struct{}{}
		}
	}
	for key := range record.HostConfig.PortBindings {
		key = strings.TrimSpace(key)
		if key != "" {
			ports[key] = struct{}{}
		}
	}
	for _, port := range dockerContainerPortMappings(record) {
		key := strings.TrimSpace(port.ContainerPort)
		if key != "" {
			ports[key] = struct{}{}
		}
	}
	return ports
}

func validDockerPortNumber(value string) bool {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return port >= 1 && port <= 65535
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
	case strings.Contains(lowerMessage, "no such image"):
		return errors.New("Docker image was not found.")
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

func dockerImageReference(repository, tag, imageID string) string {
	repository = strings.TrimSpace(repository)
	tag = strings.TrimSpace(tag)
	imageID = strings.TrimSpace(imageID)
	if repository != "" && repository != "<none>" && tag != "" && tag != "<none>" {
		return repository + ":" + tag
	}
	return imageID
}

func dockerContainerLabelsSummary(containers []dockerContainerListItem, limit int) string {
	if len(containers) == 0 {
		return ""
	}

	labels := make([]string, 0, min(len(containers), limit))
	for _, container := range containers {
		label := strings.TrimSpace(container.Name)
		if label == "" {
			label = shortDockerID(container.ID)
		}
		if label != "" {
			labels = append(labels, strconv.Quote(label))
		}
		if len(labels) == limit {
			break
		}
	}

	if len(labels) == 0 {
		return fmt.Sprintf("%d container(s)", len(containers))
	}
	if len(containers) > len(labels) {
		return strings.Join(labels, ", ") + fmt.Sprintf(", and %d more", len(containers)-len(labels))
	}
	return strings.Join(labels, ", ")
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

func parseDockerVolumeSpec(bind string) (dockerVolumeMapping, bool) {
	parts := strings.Split(strings.TrimSpace(bind), ":")
	if len(parts) < 2 {
		return dockerVolumeMapping{}, false
	}

	source := strings.TrimSpace(parts[0])
	destination := strings.TrimSpace(parts[1])
	readOnly := false
	if len(parts) > 2 {
		source = strings.TrimSpace(strings.Join(parts[:len(parts)-2], ":"))
		destination = strings.TrimSpace(parts[len(parts)-2])
		mode := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		readOnly = strings.Contains(mode, "ro")
	}

	if source == "" || destination == "" {
		return dockerVolumeMapping{}, false
	}

	return dockerVolumeMapping{
		Source:      source,
		Destination: destination,
		ReadOnly:    readOnly,
	}, true
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
