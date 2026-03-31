package files

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const maxEditableFileSize int64 = 1 << 20

var (
	ErrNotFound           = errors.New("file not found")
	ErrInvalidPath        = errors.New("invalid path")
	ErrUnsupportedEntry   = errors.New("unsupported file type")
	ErrFileExpected       = errors.New("file expected")
	ErrDirectoryExpected  = errors.New("directory expected")
	ErrBinaryFile         = errors.New("file is not editable as text")
	ErrEditableFileTooBig = errors.New("file is too large to edit")
	ErrInvalidTransfer    = errors.New("invalid transfer request")
)

type EntryType string

const (
	EntryTypeDirectory EntryType = "directory"
	EntryTypeFile      EntryType = "file"
	EntryTypeSymlink   EntryType = "symlink"
)

type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       EntryType `json:"type"`
	Extension  string    `json:"extension,omitempty"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
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
			Name:       entry.Name(),
			Path:       joinPath(normalizedPath, entry.Name()),
			Extension:  strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), "."),
			ModifiedAt: info.ModTime().UTC(),
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

func (s *Service) DownloadPath(relPath string) (string, string, error) {
	absolutePath, normalizedPath, entryType, err := s.resolveExisting(relPath)
	if err != nil {
		return "", "", err
	}
	if entryType != EntryTypeFile {
		if entryType == EntryTypeDirectory {
			return "", "", ErrFileExpected
		}
		return "", "", ErrUnsupportedEntry
	}

	return absolutePath, filepath.Base(normalizedPath), nil
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
