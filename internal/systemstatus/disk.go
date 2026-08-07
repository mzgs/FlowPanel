package systemstatus

import (
	"bufio"
	"bytes"
	"container/heap"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

const largestFileLimit = 25

type DiskSnapshot struct {
	Mounts       []DiskMount `json:"mounts"`
	LargestFiles []DiskFile  `json:"largest_files"`
	ScannedPath  string      `json:"scanned_path"`
	ScannedAt    time.Time   `json:"scanned_at"`
	ScanComplete bool        `json:"scan_complete"`
}

type DiskMount struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Filesystem  string  `json:"filesystem"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type DiskFile struct {
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at"`
}

func InspectDisk(ctx context.Context) DiskSnapshot {
	scanPath := diskUsagePath()
	mounts, skippedPaths := inspectMounts(ctx, scanPath)
	files, complete := inspectLargestFiles(ctx, scanPath, skippedPaths)
	return DiskSnapshot{
		Mounts: mounts, LargestFiles: files, ScannedPath: scanPath,
		ScannedAt: time.Now().UTC(), ScanComplete: complete,
	}
}

func inspectMounts(ctx context.Context, scanPath string) ([]DiskMount, map[string]struct{}) {
	partitions, _ := disk.PartitionsWithContext(ctx, false)
	mounts := make([]DiskMount, 0, len(partitions))
	skippedPaths := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, partition := range partitions {
		mountpoint := filepath.Clean(partition.Mountpoint)
		if mountpoint == "." || partition.Device == "" {
			continue
		}
		if _, exists := seen[mountpoint]; exists {
			continue
		}
		usage, err := disk.UsageWithContext(ctx, mountpoint)
		if err != nil || usage.Total == 0 {
			continue
		}
		seen[mountpoint] = struct{}{}
		mounts = append(mounts, DiskMount{
			Device: partition.Device, Mountpoint: mountpoint, Filesystem: partition.Fstype,
			TotalBytes: usage.Total, UsedBytes: usage.Used, FreeBytes: usage.Free, UsedPercent: usage.UsedPercent,
		})
		if !sameMountPath(mountpoint, scanPath) && pathWithin(scanPath, mountpoint) {
			skippedPaths[mountpoint] = struct{}{}
		}
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].Mountpoint < mounts[j].Mountpoint })
	return mounts, skippedPaths
}

func inspectLargestFiles(ctx context.Context, root string, skippedPaths map[string]struct{}) ([]DiskFile, bool) {
	if runtime.GOOS == "linux" {
		if files, complete, started := inspectLargestFilesWithFind(ctx, root); started {
			return files, complete
		}
	}

	files := &diskFileHeap{}
	heap.Init(files)
	complete := true
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			complete = false
			return ctx.Err()
		default:
		}
		if walkErr != nil {
			return nil
		}
		cleanPath := filepath.Clean(path)
		if entry.IsDir() {
			if cleanPath != filepath.Clean(root) && shouldSkipDiskPath(cleanPath, skippedPaths) {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		file := DiskFile{Path: cleanPath, SizeBytes: info.Size(), ModifiedAt: info.ModTime().UTC()}
		addLargestFile(files, file)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		complete = false
	}
	result := append([]DiskFile(nil), (*files)...)
	sort.Slice(result, func(i, j int) bool { return result[i].SizeBytes > result[j].SizeBytes })
	return result, complete
}

func inspectLargestFilesWithFind(ctx context.Context, root string) ([]DiskFile, bool, bool) {
	findPath, err := exec.LookPath("find")
	if err != nil {
		return nil, false, false
	}
	command := exec.CommandContext(ctx, findPath, root, "-xdev", "-type", "f", "-printf", "%s\t%T@\t%p\\0")
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return nil, false, false
	}

	files := &diskFileHeap{}
	heap.Init(files)
	reader := bufio.NewReader(stdout)
	readComplete := true
	for {
		record, readErr := reader.ReadBytes(0)
		if len(record) > 1 {
			parts := bytes.SplitN(record[:len(record)-1], []byte{'\t'}, 3)
			if len(parts) == 3 {
				size, sizeErr := strconv.ParseInt(string(parts[0]), 10, 64)
				modified, modifiedErr := strconv.ParseFloat(string(parts[1]), 64)
				if sizeErr == nil && modifiedErr == nil {
					addLargestFile(files, DiskFile{
						Path: string(parts[2]), SizeBytes: size,
						ModifiedAt: time.Unix(0, int64(modified*float64(time.Second))).UTC(),
					})
				}
			}
		}
		if readErr != nil {
			readComplete = errors.Is(readErr, io.EOF)
			break
		}
	}
	_ = command.Wait() // Permission errors are expected and do not invalidate readable results.
	result := append([]DiskFile(nil), (*files)...)
	sort.Slice(result, func(i, j int) bool { return result[i].SizeBytes > result[j].SizeBytes })
	return result, readComplete && ctx.Err() == nil, true
}

func addLargestFile(files *diskFileHeap, file DiskFile) {
	if files.Len() < largestFileLimit {
		heap.Push(files, file)
	} else if file.SizeBytes > (*files)[0].SizeBytes {
		heap.Pop(files)
		heap.Push(files, file)
	}
}

func shouldSkipDiskPath(path string, skippedPaths map[string]struct{}) bool {
	if _, skip := skippedPaths[path]; skip {
		return true
	}
	if runtime.GOOS != "linux" {
		return false
	}
	for _, virtualPath := range []string{"/dev", "/proc", "/run", "/sys"} {
		if path == virtualPath || strings.HasPrefix(path, virtualPath+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

type diskFileHeap []DiskFile

func (files diskFileHeap) Len() int           { return len(files) }
func (files diskFileHeap) Less(i, j int) bool { return files[i].SizeBytes < files[j].SizeBytes }
func (files diskFileHeap) Swap(i, j int)      { files[i], files[j] = files[j], files[i] }
func (files *diskFileHeap) Push(value any)    { *files = append(*files, value.(DiskFile)) }
func (files *diskFileHeap) Pop() any {
	old := *files
	last := old[len(old)-1]
	*files = old[:len(old)-1]
	return last
}
