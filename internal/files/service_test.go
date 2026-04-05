package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

func TestListNormalizesTraversalToRootedPath(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.Mkdir(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatalf("mkdir etc: %v", err)
	}

	listing, err := service.List("../../../../etc")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if listing.Path != "etc" {
		t.Fatalf("listing path = %q, want normalized relative path", listing.Path)
	}
	if listing.AbsolutePath != filepath.Join(service.RootPath(), "etc") {
		t.Fatalf("absolute path = %q, want rooted path", listing.AbsolutePath)
	}
}

func TestListReturnsRootWhenTraversalTargetsMissingPath(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	listing, err := service.List("../")
	if err != nil {
		t.Fatalf("list root: %v", err)
	}

	if listing.Path != "" {
		t.Fatalf("listing path = %q, want root", listing.Path)
	}
}

func TestListIncludesPermissions(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	directoryPath := filepath.Join(root, "site")
	if err := os.Mkdir(directoryPath, 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.Chmod(directoryPath, 0o750); err != nil {
		t.Fatalf("chmod site: %v", err)
	}

	filePath := filepath.Join(root, "site", "index.html")
	if err := os.WriteFile(filePath, []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chmod(filePath, 0o640); err != nil {
		t.Fatalf("chmod file: %v", err)
	}

	listing, err := service.List("")
	if err != nil {
		t.Fatalf("list root: %v", err)
	}

	if len(listing.Directories) != 1 {
		t.Fatalf("directories = %d, want 1", len(listing.Directories))
	}
	if listing.Directories[0].Permissions != "0750" {
		t.Fatalf("directory permissions = %q, want 0750", listing.Directories[0].Permissions)
	}

	listing, err = service.List("site")
	if err != nil {
		t.Fatalf("list site: %v", err)
	}

	if len(listing.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(listing.Files))
	}
	if listing.Files[0].Permissions != "0640" {
		t.Fatalf("file permissions = %q, want 0640", listing.Files[0].Permissions)
	}
}

func TestSetPermissionsUpdatesFile(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	filePath := filepath.Join(root, "index.html")
	if err := os.WriteFile(filePath, []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := service.SetPermissions("index.html", "640", false); err != nil {
		t.Fatalf("set permissions: %v", err)
	}

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("file mode = %04o, want 0640", got)
	}
}

func TestSetPermissionsRecursivelyUpdatesDirectory(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "site", "assets"), 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "site", "index.html"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "site", "assets", "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	if err := service.SetPermissions("site", "0750", true); err != nil {
		t.Fatalf("set permissions recursively: %v", err)
	}

	paths := []string{
		filepath.Join(root, "site"),
		filepath.Join(root, "site", "assets"),
		filepath.Join(root, "site", "index.html"),
		filepath.Join(root, "site", "assets", "app.js"),
	}
	for _, currentPath := range paths {
		info, err := os.Stat(currentPath)
		if err != nil {
			t.Fatalf("stat %s: %v", currentPath, err)
		}
		if got := info.Mode().Perm(); got != 0o750 {
			t.Fatalf("%s mode = %04o, want 0750", currentPath, got)
		}
	}
}

func TestSetPermissionsRejectsInvalidMode(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	filePath := filepath.Join(root, "index.html")
	if err := os.WriteFile(filePath, []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := service.SetPermissions("index.html", "999", false); err != ErrInvalidPermissions {
		t.Fatalf("set permissions error = %v, want %v", err, ErrInvalidPermissions)
	}
}

func TestFileLifecycle(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.CreateDirectory("", "site"); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := service.CreateFile("site", "index.html"); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := service.WriteTextFile("site/index.html", "<h1>hello</h1>"); err != nil {
		t.Fatalf("write text file: %v", err)
	}

	content, err := service.ReadTextFile("site/index.html")
	if err != nil {
		t.Fatalf("read text file: %v", err)
	}
	if content.Content != "<h1>hello</h1>" {
		t.Fatalf("content = %q, want saved value", content.Content)
	}

	newPath, err := service.Rename("site/index.html", "home.html")
	if err != nil {
		t.Fatalf("rename file: %v", err)
	}
	if newPath != "site/home.html" {
		t.Fatalf("new path = %q, want site/home.html", newPath)
	}

	if err := service.Delete("site/home.html"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "site", "home.html")); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}
}

func TestReadTextFileRejectsBinary(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	path := filepath.Join(root, "binary.dat")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := service.ReadTextFile("binary.dat"); err != ErrBinaryFile {
		t.Fatalf("read error = %v, want %v", err, ErrBinaryFile)
	}
}

func TestUploadCopiesMultipartFiles(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "note.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello upload")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	reader := multipart.NewReader(&body, writer.Boundary())
	form, err := reader.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("read form: %v", err)
	}

	if err := service.Upload("", form.File["files"]); err != nil {
		t.Fatalf("upload: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(data) != "hello upload" {
		t.Fatalf("uploaded content = %q, want hello upload", string(data))
	}
}

func TestTransferMovesAndCopiesEntries(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.CreateDirectory("", "source"); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := service.CreateDirectory("", "target"); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "move.txt"), []byte("move"), 0o644); err != nil {
		t.Fatalf("seed move file: %v", err)
	}

	if err := service.Transfer("copy", []string{"source/note.txt"}, "target"); err != nil {
		t.Fatalf("copy transfer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "target", "note.txt")); err != nil {
		t.Fatalf("copied file missing: %v", err)
	}

	if err := service.Transfer("move", []string{"source/move.txt"}, "target"); err != nil {
		t.Fatalf("move transfer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "source", "move.txt")); !os.IsNotExist(err) {
		t.Fatalf("moved file still exists in source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "target", "move.txt")); err != nil {
		t.Fatalf("moved file missing in target: %v", err)
	}
}

func TestTransferRejectsMovingFolderIntoItself(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.CreateDirectory("", "parent"); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := service.CreateDirectory("parent", "child"); err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := service.Transfer("move", []string{"parent"}, "parent/child"); err != ErrInvalidTransfer {
		t.Fatalf("transfer error = %v, want %v", err, ErrInvalidTransfer)
	}
}

func TestDownloadPathReturnsFileWithoutCleanup(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	path := filepath.Join(service.RootPath(), "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	downloadPath, name, cleanup, err := service.DownloadPath("note.txt")
	if err != nil {
		t.Fatalf("download path: %v", err)
	}

	if downloadPath != path {
		t.Fatalf("download path = %q, want %q", downloadPath, path)
	}
	if name != "note.txt" {
		t.Fatalf("download name = %q, want note.txt", name)
	}

	cleanup()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing after cleanup: %v", err)
	}
}

func TestDownloadPathArchivesDirectory(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.Mkdir(filepath.Join(root, "site"), 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "site", "index.html"), []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatalf("write site file: %v", err)
	}

	downloadPath, name, cleanup, err := service.DownloadPath("site")
	if err != nil {
		t.Fatalf("download path: %v", err)
	}

	if name != "site.tar.gz" {
		t.Fatalf("download name = %q, want site.tar.gz", name)
	}

	entries := readArchiveEntries(t, downloadPath)
	if got := string(entries["site/index.html"]); got != "<h1>hello</h1>" {
		t.Fatalf("archive entry = %q, want site content", got)
	}

	cleanup()
	if _, err := os.Stat(downloadPath); !os.IsNotExist(err) {
		t.Fatalf("archive still exists after cleanup: %v", err)
	}
}

func TestPrepareDownloadPathsStreamsSelectionArchive(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.Mkdir(filepath.Join(root, "site"), 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "site", "index.html"), []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatalf("write site file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "robots.txt"), []byte("User-agent: *"), 0o644); err != nil {
		t.Fatalf("write robots file: %v", err)
	}

	name, writeArchive, err := service.PrepareDownloadPaths([]string{"site", "robots.txt"})
	if err != nil {
		t.Fatalf("prepare download paths: %v", err)
	}
	if name != "download.tar.gz" {
		t.Fatalf("archive name = %q, want download.tar.gz", name)
	}

	var archive bytes.Buffer
	if err := writeArchive(&archive); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	entries := readArchiveEntriesFromReader(t, bytes.NewReader(archive.Bytes()))
	if got := string(entries["site/index.html"]); got != "<h1>hello</h1>" {
		t.Fatalf("site/index.html = %q, want site content", got)
	}
	if got := string(entries["robots.txt"]); got != "User-agent: *" {
		t.Fatalf("robots.txt = %q, want robots content", got)
	}
}

func TestPrepareDownloadPathsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if _, _, err := service.PrepareDownloadPaths([]string{"link.txt"}); err != ErrUnsupportedEntry {
		t.Fatalf("prepare download error = %v, want %v", err, ErrUnsupportedEntry)
	}
}

func TestCreateArchiveCreatesTarballInDestination(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.Mkdir(filepath.Join(root, "site"), 0o755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "site", "index.html"), []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatalf("write site file: %v", err)
	}

	archivePath, err := service.CreateArchive([]string{"site"}, "")
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	if archivePath != "site.tar.gz" {
		t.Fatalf("archive path = %q, want site.tar.gz", archivePath)
	}

	entries := readArchiveEntries(t, filepath.Join(root, archivePath))
	if got := string(entries["site/index.html"]); got != "<h1>hello</h1>" {
		t.Fatalf("site/index.html = %q, want site content", got)
	}
}

func TestExtractArchiveExtractsTarGzIntoParentDirectory(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar.gz")
	writeTarGzArchive(t, archivePath, map[string]string{
		"bundle/index.html": "<h1>hello</h1>",
	})

	if err := service.ExtractArchive("bundle.tar.gz"); err != nil {
		t.Fatalf("extract archive: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "bundle", "index.html"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "<h1>hello</h1>" {
		t.Fatalf("extracted content = %q, want site content", string(data))
	}
}

func TestExtractArchiveExtractsZipIntoParentDirectory(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.zip")
	writeZipArchive(t, archivePath, map[string]string{
		"bundle/app.js": "console.log('ok')",
	})

	if err := service.ExtractArchive("bundle.zip"); err != nil {
		t.Fatalf("extract zip archive: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "bundle", "app.js"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "console.log('ok')" {
		t.Fatalf("extracted content = %q, want zip content", string(data))
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.tar.gz")
	writeTarGzArchive(t, archivePath, map[string]string{
		"../evil.txt": "nope",
	})

	if err := service.ExtractArchive("bundle.tar.gz"); err != ErrInvalidArchive {
		t.Fatalf("extract archive error = %v, want %v", err, ErrInvalidArchive)
	}
	if _, err := os.Stat(filepath.Join(root, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("unexpected file extracted: %v", err)
	}
}

func TestExtractArchiveRejectsConflictingTopLevelEntry(t *testing.T) {
	root := t.TempDir()
	service, err := NewService(root)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := os.Mkdir(filepath.Join(root, "bundle"), 0o755); err != nil {
		t.Fatalf("mkdir existing bundle: %v", err)
	}

	archivePath := filepath.Join(root, "bundle.zip")
	writeZipArchive(t, archivePath, map[string]string{
		"bundle/app.js": "console.log('ok')",
	})

	if err := service.ExtractArchive("bundle.zip"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("extract archive error = %v, want %v", err, fs.ErrExist)
	}
	if _, err := os.Stat(filepath.Join(root, "bundle", "app.js")); !os.IsNotExist(err) {
		t.Fatalf("archive should not partially extract: %v", err)
	}
}

func readArchiveEntries(t *testing.T, archivePath string) map[string][]byte {
	t.Helper()

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip archive: %v", err)
	}
	defer gzipReader.Close()

	return readArchiveEntriesFromGzip(t, gzipReader)
}

func readArchiveEntriesFromReader(t *testing.T, reader io.Reader) map[string][]byte {
	t.Helper()

	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		t.Fatalf("open gzip archive: %v", err)
	}
	defer gzipReader.Close()

	return readArchiveEntriesFromGzip(t, gzipReader)
}

func readArchiveEntriesFromGzip(t *testing.T, reader io.Reader) map[string][]byte {
	t.Helper()

	tarReader := tar.NewReader(reader)
	entries := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar archive: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		payload, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read archive entry %q: %v", header.Name, err)
		}
		entries[header.Name] = payload
	}

	return entries
}

func writeTarGzArchive(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("write tar payload: %v", err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
}

func writeZipArchive(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()

	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip archive: %v", err)
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)

	for name, content := range files {
		entryWriter, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := entryWriter.Write([]byte(content)); err != nil {
			t.Fatalf("write zip payload: %v", err)
		}
	}

	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
}
