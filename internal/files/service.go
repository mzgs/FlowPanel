package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxEditableFileSize int64 = 1 << 20

var (
	ErrNotFound           = errors.New("file not found")
	ErrInvalidPath        = errors.New("invalid path")
	ErrUnsupportedEntry   = errors.New("unsupported file type")
	ErrUnsupportedArchive = errors.New("unsupported archive")
	ErrInvalidArchive     = errors.New("invalid archive")
	ErrFileExpected       = errors.New("file expected")
	ErrDirectoryExpected  = errors.New("directory expected")
	ErrBinaryFile         = errors.New("file is not editable as text")
	ErrEditableFileTooBig = errors.New("file is too large to edit")
	ErrInvalidTransfer    = errors.New("invalid transfer request")
	ErrInvalidPermissions = errors.New("invalid permissions")
)

type EntryType string

const (
	EntryTypeDirectory EntryType = "directory"
	EntryTypeFile      EntryType = "file"
	EntryTypeSymlink   EntryType = "symlink"
)

type Entry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Type        EntryType `json:"type"`
	Extension   string    `json:"extension,omitempty"`
	Permissions string    `json:"permissions"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modified_at"`
}

type Listing struct {
	RootName     string  `json:"root_name"`
	RootPath     string  `json:"root_path"`
	Path         string  `json:"path"`
	ParentPath   string  `json:"parent_path,omitempty"`
	AbsolutePath string  `json:"absolute_path"`
	Directories  []Entry `json:"directories"`
	Files        []Entry `json:"files"`
}

type FileContent struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Extension  string    `json:"extension,omitempty"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Content    string    `json:"content"`
}

type Service struct {
	rootPath string
	rootName string
}

func NewService(rootPath string) (*Service, error) {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return nil, fmt.Errorf("%w: root path is required", ErrInvalidPath)
	}

	rootPath = filepath.Clean(rootPath)
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return nil, fmt.Errorf("ensure file root: %w", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve file root: %w", err)
	}

	rootName := filepath.Base(resolvedRoot)
	if rootName == "." || rootName == string(filepath.Separator) {
		rootName = resolvedRoot
	}

	return &Service{
		rootPath: resolvedRoot,
		rootName: rootName,
	}, nil
}

func (s *Service) RootPath() string {
	return s.rootPath
}

func (s *Service) RootName() string {
	return s.rootName
}

func (s *Service) List(relPath string) (Listing, error) {
	absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return Listing{}, err
	}
	if entryType != EntryTypeDirectory {
		return Listing{}, ErrDirectoryExpected
	}

	entries, err := os.ReadDir(absolutePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Listing{}, ErrNotFound
		}
		return Listing{}, err
	}

	listing := Listing{
		RootName:     s.rootName,
		RootPath:     s.rootPath,
		Path:         normalizedPath,
		AbsolutePath: absolutePath,
		Directories:  make([]Entry, 0),
		Files:        make([]Entry, 0),
	}
	if normalizedPath != "" {
		listing.ParentPath = parentPath(normalizedPath)
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		item := Entry{
			Name:        entry.Name(),
			Path:        joinPath(normalizedPath, entry.Name()),
			Extension:   strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), "."),
			Permissions: fmt.Sprintf("%04o", info.Mode().Perm()),
			ModifiedAt:  info.ModTime().UTC(),
		}

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			item.Type = EntryTypeSymlink
			item.Size = info.Size()
			listing.Files = append(listing.Files, item)
		case entry.IsDir():
			item.Type = EntryTypeDirectory
			listing.Directories = append(listing.Directories, item)
		default:
			item.Type = EntryTypeFile
			item.Size = info.Size()
			listing.Files = append(listing.Files, item)
		}
	}

	sortEntries(listing.Directories)
	sortEntries(listing.Files)

	return listing, nil
}

func (s *Service) CreateDirectory(relPath string, name string) error {
	parentAbsolutePath, _, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return err
	}
	if entryType != EntryTypeDirectory {
		return ErrDirectoryExpected
	}

	baseName, err := validateBaseName(name)
	if err != nil {
		return err
	}

	targetPath := filepath.Join(parentAbsolutePath, filepath.FromSlash(baseName))
	if err := ensureWithinRoot(s.rootPath, targetPath); err != nil {
		return err
	}
	if _, err := os.Stat(targetPath); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.Mkdir(targetPath, 0o755); err != nil {
		return err
	}

	return nil
}

func (s *Service) CreateFile(relPath string, name string) error {
	parentAbsolutePath, _, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return err
	}
	if entryType != EntryTypeDirectory {
		return ErrDirectoryExpected
	}

	baseName, err := validateBaseName(name)
	if err != nil {
		return err
	}

	targetPath := filepath.Join(parentAbsolutePath, filepath.FromSlash(baseName))
	if err := ensureWithinRoot(s.rootPath, targetPath); err != nil {
		return err
	}

	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	return file.Close()
}

func (s *Service) Rename(relPath string, name string) (string, error) {
	absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return "", err
	}
	if normalizedPath == "" {
		return "", ErrInvalidPath
	}
	if entryType == EntryTypeSymlink {
		return "", ErrUnsupportedEntry
	}

	baseName, err := validateBaseName(name)
	if err != nil {
		return "", err
	}

	parentAbsolutePath := filepath.Dir(absolutePath)
	targetPath := filepath.Join(parentAbsolutePath, filepath.FromSlash(baseName))
	if err := ensureWithinRoot(s.rootPath, targetPath); err != nil {
		return "", err
	}
	if _, err := os.Stat(targetPath); err == nil {
		return "", fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	if err := os.Rename(absolutePath, targetPath); err != nil {
		return "", err
	}

	return joinPath(parentPath(normalizedPath), baseName), nil
}

func (s *Service) Delete(relPath string) error {
	absolutePath, normalizedPath, err := s.resolvePath(relPath)
	if err != nil {
		return err
	}
	if normalizedPath == "" {
		return ErrInvalidPath
	}

	info, err := os.Lstat(absolutePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsupportedEntry
	}
	if info.IsDir() {
		return os.RemoveAll(absolutePath)
	}
	return os.Remove(absolutePath)
}

func (s *Service) ReadTextFile(relPath string) (FileContent, error) {
	absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return FileContent{}, err
	}
	if entryType != EntryTypeFile {
		if entryType == EntryTypeDirectory {
			return FileContent{}, ErrFileExpected
		}
		return FileContent{}, ErrUnsupportedEntry
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return FileContent{}, ErrNotFound
		}
		return FileContent{}, err
	}
	if info.Size() > maxEditableFileSize {
		return FileContent{}, ErrEditableFileTooBig
	}

	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return FileContent{}, err
	}
	if !isTextContent(data) {
		return FileContent{}, ErrBinaryFile
	}

	return FileContent{
		Name:       filepath.Base(absolutePath),
		Path:       normalizedPath,
		Extension:  strings.TrimPrefix(strings.ToLower(filepath.Ext(absolutePath)), "."),
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
		Content:    string(data),
	}, nil
}

func (s *Service) WriteTextFile(relPath string, content string) error {
	absolutePath, _, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return err
	}
	if entryType != EntryTypeFile {
		if entryType == EntryTypeDirectory {
			return ErrFileExpected
		}
		return ErrUnsupportedEntry
	}

	if !utf8.ValidString(content) {
		return ErrBinaryFile
	}

	return os.WriteFile(absolutePath, []byte(content), 0o644)
}

func (s *Service) SetPermissions(relPath string, permissions string, recursive bool) error {
	absolutePath, _, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return err
	}
	if entryType == EntryTypeSymlink {
		return ErrUnsupportedEntry
	}

	mode, err := parsePermissionMode(permissions)
	if err != nil {
		return err
	}

	if entryType != EntryTypeDirectory || !recursive {
		return os.Chmod(absolutePath, mode)
	}

	paths := make([]string, 0, 16)
	if err := filepath.WalkDir(absolutePath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}

		paths = append(paths, currentPath)
		return nil
	}); err != nil {
		return err
	}

	for index := len(paths) - 1; index >= 0; index-- {
		if err := os.Chmod(paths[index], mode); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) Upload(relPath string, headers []*multipart.FileHeader) error {
	parentAbsolutePath, _, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return err
	}
	if entryType != EntryTypeDirectory {
		return ErrDirectoryExpected
	}

	for _, header := range headers {
		if header == nil {
			continue
		}

		baseName, err := validateBaseName(header.Filename)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(parentAbsolutePath, filepath.FromSlash(baseName))
		if err := ensureWithinRoot(s.rootPath, targetPath); err != nil {
			return err
		}
		if _, err := os.Stat(targetPath); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		if err := copyUploadedFile(targetPath, header); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) DownloadPath(relPath string) (string, string, func(), error) {
	absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return "", "", nil, err
	}

	switch entryType {
	case EntryTypeFile:
		return absolutePath, filepath.Base(normalizedPath), func() {}, nil
	case EntryTypeDirectory:
		archivePath, archiveName, err := createDirectoryArchive(absolutePath, normalizedPath, s.rootName)
		if err != nil {
			return "", "", nil, err
		}

		return archivePath, archiveName, func() {
			_ = os.Remove(archivePath)
		}, nil
	default:
		return "", "", nil, ErrUnsupportedEntry
	}
}

func (s *Service) PrepareDownloadPaths(relPaths []string) (string, func(io.Writer) error, error) {
	sources, archiveRoot, archiveBaseName, err := s.resolveDownloadSources(relPaths)
	if err != nil {
		return "", nil, err
	}

	return archiveBaseName + ".tar.gz", func(writer io.Writer) error {
		return writeSelectionArchive(writer, sources, archiveRoot, s.rootPath)
	}, nil
}

func (s *Service) CreateArchive(relPaths []string, destination string) (string, error) {
	destinationAbsolutePath, destinationNormalizedPath, entryType, err := s.resolveExisting(destination)
	if err != nil {
		return "", err
	}
	if entryType != EntryTypeDirectory {
		return "", ErrDirectoryExpected
	}

	sources, _, archiveBaseName, err := s.resolveDownloadSources(relPaths)
	if err != nil {
		return "", err
	}

	if len(sources) == 1 {
		archiveBaseName = filepath.Base(sources[0].normalizedPath)
	} else if archiveBaseName == "download" {
		archiveBaseName = "archive"
	}

	archiveFileName, archiveAbsolutePath, err := nextAvailableArchivePath(destinationAbsolutePath, archiveBaseName)
	if err != nil {
		return "", err
	}

	file, err := os.OpenFile(archiveAbsolutePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}

	success := false
	defer func() {
		if file != nil {
			if err := file.Close(); err != nil && !success {
				_ = os.Remove(archiveAbsolutePath)
			}
		}
		if !success {
			_ = os.Remove(archiveAbsolutePath)
		}
	}()

	if err := writeSelectionArchive(file, sources, archiveRootForSources(sources), s.rootPath); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	file = nil

	success = true

	return joinPath(destinationNormalizedPath, archiveFileName), nil
}

func (s *Service) ExtractArchive(relPath string) error {
	absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return err
	}
	if entryType != EntryTypeFile {
		if entryType == EntryTypeDirectory {
			return ErrFileExpected
		}
		return ErrUnsupportedEntry
	}

	destinationAbsolutePath := filepath.Dir(absolutePath)
	stagingPath, err := os.MkdirTemp(destinationAbsolutePath, ".flowpanel-extract-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingPath)

	if err := extractArchiveToDirectory(absolutePath, filepath.Base(normalizedPath), stagingPath); err != nil {
		return err
	}

	entries, err := os.ReadDir(stagingPath)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return ErrInvalidArchive
	}

	for _, entry := range entries {
		targetPath := filepath.Join(destinationAbsolutePath, entry.Name())
		if _, err := os.Lstat(targetPath); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}

	for _, entry := range entries {
		sourcePath := filepath.Join(stagingPath, entry.Name())
		targetPath := filepath.Join(destinationAbsolutePath, entry.Name())
		if err := movePath(sourcePath, targetPath); err != nil {
			return err
		}
	}

	return nil
}

type downloadSource struct {
	absolutePath   string
	normalizedPath string
	entryType      EntryType
}

func (s *Service) resolveDownloadSources(relPaths []string) ([]downloadSource, string, string, error) {
	sources := make([]downloadSource, 0, len(relPaths))
	seen := make(map[string]struct{}, len(relPaths))

	for _, relPath := range relPaths {
		absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
		if err != nil {
			return nil, "", "", err
		}
		if normalizedPath == "" || entryType == EntryTypeSymlink {
			return nil, "", "", ErrUnsupportedEntry
		}
		if _, exists := seen[normalizedPath]; exists {
			continue
		}

		seen[normalizedPath] = struct{}{}
		sources = append(sources, downloadSource{
			absolutePath:   absolutePath,
			normalizedPath: normalizedPath,
			entryType:      entryType,
		})
	}

	if len(sources) == 0 {
		return nil, "", "", ErrInvalidPath
	}

	sort.Slice(sources, func(i int, j int) bool {
		return sources[i].normalizedPath < sources[j].normalizedPath
	})

	filtered := make([]downloadSource, 0, len(sources))
	for _, source := range sources {
		skip := false
		for _, existing := range filtered {
			if existing.entryType != EntryTypeDirectory {
				continue
			}
			if source.normalizedPath == existing.normalizedPath || isArchiveChildPath(source.normalizedPath, existing.normalizedPath) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, source)
		}
	}

	archiveBaseName := "download"
	if len(filtered) == 1 {
		archiveBaseName = filepath.Base(filtered[0].normalizedPath)
	}

	archiveRoot := commonArchiveRoot(filtered)

	return filtered, archiveRoot, archiveBaseName, nil
}

func createDirectoryArchive(absolutePath, normalizedPath, rootName string) (string, string, error) {
	archiveBaseName := filepath.Base(normalizedPath)
	if normalizedPath == "" {
		archiveBaseName = rootName
	}
	if archiveBaseName == "." || archiveBaseName == string(filepath.Separator) || archiveBaseName == "" {
		archiveBaseName = "download"
	}

	file, err := os.CreateTemp("", "flowpanel-download-*.tar.gz")
	if err != nil {
		return "", "", fmt.Errorf("create download archive: %w", err)
	}

	archivePath := file.Name()
	success := false
	defer func() {
		if err := file.Close(); err != nil && !success {
			_ = os.Remove(archivePath)
		}
		if !success {
			_ = os.Remove(archivePath)
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	rootParent := filepath.Dir(absolutePath)
	walkErr := filepath.WalkDir(absolutePath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := os.Lstat(currentPath)
		if err != nil {
			return err
		}

		archiveEntryPath, err := filepath.Rel(rootParent, currentPath)
		if err != nil {
			return err
		}

		return writeTarEntry(tarWriter, currentPath, archiveEntryPath, info)
	})
	closeTarErr := tarWriter.Close()
	closeGzipErr := gzipWriter.Close()
	closeFileErr := file.Close()

	if walkErr != nil {
		return "", "", fmt.Errorf("archive directory: %w", walkErr)
	}
	if closeTarErr != nil {
		return "", "", fmt.Errorf("close tar archive: %w", closeTarErr)
	}
	if closeGzipErr != nil {
		return "", "", fmt.Errorf("close gzip archive: %w", closeGzipErr)
	}
	if closeFileErr != nil {
		return "", "", fmt.Errorf("close archive file: %w", closeFileErr)
	}

	success = true

	return archivePath, archiveBaseName + ".tar.gz", nil
}

func writeSelectionArchive(writer io.Writer, sources []downloadSource, archiveRoot string, rootPath string) error {
	gzipWriter := gzip.NewWriter(writer)
	tarWriter := tar.NewWriter(gzipWriter)
	archiveRootPath := rootPath
	if archiveRoot != "" {
		archiveRootPath = filepath.Join(rootPath, filepath.FromSlash(archiveRoot))
	}
	for _, source := range sources {
		walkErr := filepath.WalkDir(source.absolutePath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			info, err := os.Lstat(currentPath)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return ErrUnsupportedEntry
			}

			relativePath, err := filepath.Rel(source.absolutePath, currentPath)
			if err != nil {
				return err
			}

			if source.entryType == EntryTypeDirectory && relativePath == "." {
				if source.absolutePath == archiveRootPath {
					return nil
				}
			}

			archiveEntryPath, err := filepath.Rel(archiveRootPath, currentPath)
			if err != nil {
				return err
			}

			return writeTarEntry(tarWriter, currentPath, filepath.ToSlash(archiveEntryPath), info)
		})
		if walkErr != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return fmt.Errorf("archive selection: %w", walkErr)
		}
	}

	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar archive: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip archive: %w", err)
	}

	return nil
}

func archiveRootForSources(sources []downloadSource) string {
	if len(sources) == 0 {
		return ""
	}
	if len(sources) == 1 {
		return parentPath(sources[0].normalizedPath)
	}

	return commonArchiveRoot(sources)
}

func writeTarEntry(tarWriter *tar.Writer, sourcePath, archivePath string, info fs.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("build tar header for %q: %w", sourcePath, err)
	}

	header.Name = filepath.ToSlash(strings.TrimPrefix(archivePath, "./"))
	if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
		header.Name += "/"
	}

	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("read symlink %q: %w", sourcePath, err)
		}
		header.Linkname = linkTarget
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %q: %w", sourcePath, err)
	}

	if !info.Mode().IsRegular() {
		return nil
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open download source %q: %w", sourcePath, err)
	}
	defer file.Close()

	if _, err := io.Copy(tarWriter, file); err != nil {
		return fmt.Errorf("write download source %q: %w", sourcePath, err)
	}

	return nil
}

func (s *Service) Transfer(mode string, sources []string, target string) error {
	targetAbsolutePath, targetNormalizedPath, entryType, err := s.resolveExisting(target)
	if err != nil {
		return err
	}
	if entryType != EntryTypeDirectory {
		return ErrDirectoryExpected
	}

	if mode != "copy" && mode != "move" {
		return ErrInvalidTransfer
	}

	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source) == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}

		sourceAbsolutePath, sourceNormalizedPath, sourceType, err := s.resolveExisting(source)
		if err != nil {
			return err
		}
		if sourceType == EntryTypeSymlink || sourceNormalizedPath == "" {
			return ErrInvalidTransfer
		}

		baseName := filepath.Base(sourceAbsolutePath)
		destinationAbsolutePath := filepath.Join(targetAbsolutePath, baseName)
		if err := ensureWithinRoot(s.rootPath, destinationAbsolutePath); err != nil {
			return err
		}
		if sourceNormalizedPath == targetNormalizedPath {
			return ErrInvalidTransfer
		}
		if sourceType == EntryTypeDirectory && isNestedPath(destinationAbsolutePath, sourceAbsolutePath) {
			return ErrInvalidTransfer
		}
		if _, err := os.Stat(destinationAbsolutePath); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		switch mode {
		case "copy":
			if err := copyPath(sourceAbsolutePath, destinationAbsolutePath); err != nil {
				return err
			}
		case "move":
			if err := movePath(sourceAbsolutePath, destinationAbsolutePath); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) resolveExisting(relPath string) (string, string, EntryType, error) {
	absolutePath, normalizedPath, err := s.resolvePath(relPath)
	if err != nil {
		return "", "", "", err
	}

	info, err := os.Lstat(absolutePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", "", ErrNotFound
		}
		return "", "", "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return absolutePath, normalizedPath, EntryTypeSymlink, nil
	}
	if info.IsDir() {
		return absolutePath, normalizedPath, EntryTypeDirectory, nil
	}
	return absolutePath, normalizedPath, EntryTypeFile, nil
}

func (s *Service) resolvePath(relPath string) (string, string, error) {
	normalizedPath := normalizeRelativePath(relPath)
	absolutePath := filepath.Join(s.rootPath, filepath.FromSlash(normalizedPath))
	absolutePath = filepath.Clean(absolutePath)

	if err := ensureWithinRoot(s.rootPath, absolutePath); err != nil {
		return "", "", err
	}

	return absolutePath, normalizedPath, nil
}

func normalizeRelativePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}

	cleaned := path.Clean("/" + value)
	if cleaned == "/" || cleaned == "." {
		return ""
	}

	return strings.TrimPrefix(cleaned, "/")
}

func ensureWithinRoot(rootPath string, targetPath string) error {
	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return ErrInvalidPath
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return ErrInvalidPath
	}
	return nil
}

func validateBaseName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrInvalidPath
	}
	if value == "." || value == ".." {
		return "", ErrInvalidPath
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return "", ErrInvalidPath
	}
	return value, nil
}

func parentPath(value string) string {
	if value == "" {
		return ""
	}
	parent := path.Dir("/" + value)
	if parent == "/" || parent == "." {
		return ""
	}
	return strings.TrimPrefix(parent, "/")
}

func joinPath(base string, name string) string {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/")
	if base == "" {
		return name
	}
	return path.Join(base, name)
}

func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i int, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func isTextContent(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if int64(len(data)) > maxEditableFileSize {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	return utf8.Valid(data)
}

func parsePermissionMode(value string) (fs.FileMode, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 3 || len(trimmed) > 4 {
		return 0, ErrInvalidPermissions
	}
	for _, char := range trimmed {
		if char < '0' || char > '7' {
			return 0, ErrInvalidPermissions
		}
	}

	parsed, err := strconv.ParseUint(trimmed, 8, 32)
	if err != nil {
		return 0, ErrInvalidPermissions
	}

	return fs.FileMode(parsed), nil
}

func copyUploadedFile(targetPath string, header *multipart.FileHeader) error {
	source, err := header.Open()
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = target.Close()
	}()

	if _, err := io.Copy(target, source); err != nil {
		return err
	}

	return nil
}

func movePath(sourcePath string, destinationPath string) error {
	if err := os.Rename(sourcePath, destinationPath); err == nil {
		return nil
	} else if err != nil && !isCrossDeviceLinkError(err) {
		return err
	}

	if err := copyPath(sourcePath, destinationPath); err != nil {
		return err
	}

	return os.RemoveAll(sourcePath)
}

func copyPath(sourcePath string, destinationPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return copyDirectory(sourcePath, destinationPath, info.Mode())
	}

	return copyFile(sourcePath, destinationPath, info.Mode())
}

func copyDirectory(sourcePath string, destinationPath string, mode fs.FileMode) error {
	if err := os.Mkdir(destinationPath, mode.Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		sourceChildPath := filepath.Join(sourcePath, entry.Name())
		destinationChildPath := filepath.Join(destinationPath, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrUnsupportedEntry
		}
		if err := copyPath(sourceChildPath, destinationChildPath); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(sourcePath string, destinationPath string, mode fs.FileMode) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer func() {
		_ = destinationFile.Close()
	}()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}

	return nil
}

func nextAvailableArchivePath(directoryPath string, baseName string) (string, string, error) {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" || baseName == "." {
		baseName = "archive"
	}

	for index := 0; index < 10_000; index++ {
		fileName := baseName + ".tar.gz"
		if index > 0 {
			fileName = fmt.Sprintf("%s-%d.tar.gz", baseName, index+1)
		}

		absolutePath := filepath.Join(directoryPath, fileName)
		if err := ensureWithinRoot(directoryPath, absolutePath); err != nil {
			return "", "", err
		}
		if _, err := os.Lstat(absolutePath); errors.Is(err, fs.ErrNotExist) {
			return fileName, absolutePath, nil
		} else if err != nil {
			return "", "", err
		}
	}

	return "", "", fs.ErrExist
}

func extractArchiveToDirectory(archivePath string, archiveName string, destinationPath string) error {
	switch detectArchiveFormat(archiveName) {
	case "tar.gz":
		file, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer file.Close()

		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return ErrInvalidArchive
		}
		defer gzipReader.Close()

		return extractTarStream(gzipReader, destinationPath)
	case "tar":
		file, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer file.Close()

		return extractTarStream(file, destinationPath)
	case "zip":
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return ErrInvalidArchive
		}
		defer reader.Close()

		return extractZipArchive(&reader.Reader, destinationPath)
	default:
		return ErrUnsupportedArchive
	}
}

func detectArchiveFormat(fileName string) string {
	normalized := strings.ToLower(strings.TrimSpace(fileName))
	switch {
	case strings.HasSuffix(normalized, ".tar.gz"), strings.HasSuffix(normalized, ".tgz"):
		return "tar.gz"
	case strings.HasSuffix(normalized, ".tar"):
		return "tar"
	case strings.HasSuffix(normalized, ".zip"):
		return "zip"
	default:
		return ""
	}
}

func extractTarStream(reader io.Reader, destinationPath string) error {
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return ErrInvalidArchive
		}

		targetPath, err := resolveArchiveEntryTarget(destinationPath, header.Name)
		if err != nil {
			return err
		}
		if targetPath == "" {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := createArchiveDirectory(targetPath, header.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := writeArchiveFile(targetPath, tarReader, header.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeXHeader, tar.TypeXGlobalHeader:
			continue
		default:
			return ErrUnsupportedArchive
		}
	}
}

func extractZipArchive(reader *zip.Reader, destinationPath string) error {
	for _, file := range reader.File {
		targetPath, err := resolveArchiveEntryTarget(destinationPath, file.Name)
		if err != nil {
			return err
		}
		if targetPath == "" {
			continue
		}

		mode := file.Mode()
		if mode&os.ModeSymlink != 0 {
			return ErrUnsupportedArchive
		}

		if file.FileInfo().IsDir() {
			if err := createArchiveDirectory(targetPath, mode); err != nil {
				return err
			}
			continue
		}

		source, err := file.Open()
		if err != nil {
			return ErrInvalidArchive
		}

		writeErr := writeArchiveFile(targetPath, source, mode)
		closeErr := source.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return ErrInvalidArchive
		}
	}

	return nil
}

func resolveArchiveEntryTarget(rootPath string, entryName string) (string, error) {
	normalizedName := strings.TrimSpace(strings.ReplaceAll(entryName, "\\", "/"))
	if normalizedName == "" {
		return "", nil
	}

	targetPath := filepath.Join(rootPath, filepath.FromSlash(normalizedName))
	if err := ensureWithinRoot(rootPath, targetPath); err != nil {
		return "", ErrInvalidArchive
	}
	targetPath = filepath.Clean(targetPath)

	relativePath, err := filepath.Rel(rootPath, targetPath)
	if err != nil {
		return "", ErrInvalidArchive
	}
	if relativePath == "." {
		return "", nil
	}

	return targetPath, nil
}

func createArchiveDirectory(targetPath string, mode fs.FileMode) error {
	permissions := mode.Perm()
	if permissions == 0 {
		permissions = 0o755
	}

	return os.MkdirAll(targetPath, permissions)
}

func writeArchiveFile(targetPath string, source io.Reader, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	permissions := mode.Perm()
	if permissions == 0 {
		permissions = 0o644
	}

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, permissions)
	if err != nil {
		return err
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return ErrInvalidArchive
	}

	return nil
}

func isCrossDeviceLinkError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "cross-device link")
}

func isNestedPath(childPath string, parentPath string) bool {
	relativePath, err := filepath.Rel(parentPath, childPath)
	if err != nil {
		return false
	}
	return relativePath == "." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) == false
}

func isArchiveChildPath(childPath string, parentPath string) bool {
	return childPath == parentPath || strings.HasPrefix(childPath, parentPath+"/")
}

func commonArchiveRoot(sources []downloadSource) string {
	if len(sources) == 0 {
		return ""
	}

	common := splitArchivePath(sources[0].normalizedPath)
	for _, source := range sources[1:] {
		parts := splitArchivePath(source.normalizedPath)
		limit := len(common)
		if len(parts) < limit {
			limit = len(parts)
		}
		index := 0
		for index < limit && common[index] == parts[index] {
			index++
		}
		common = common[:index]
		if len(common) == 0 {
			return ""
		}
	}

	return strings.Join(common, "/")
}

func splitArchivePath(value string) []string {
	if value == "" {
		return nil
	}

	return strings.Split(value, "/")
}
