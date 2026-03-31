package files

import (
	"bytes"
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
