package attachments

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hadi77ir/flatnotes-go/internal/storage"
)

func TestFileSystemServiceCreateAndOpen(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFileSystemService(storage.NewOSFileSystem(root))
	if err != nil {
		t.Fatalf("NewFileSystemService() error = %v", err)
	}
	resp, err := svc.Create("image name.png", []byte("data"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if resp.Filename != "image name.png" {
		t.Fatalf("Filename = %q", resp.Filename)
	}
	if resp.URL != "attachments/image%20name.png" {
		t.Fatalf("URL = %q", resp.URL)
	}
	opened, err := svc.Open(resp.Filename)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	data, err := os.ReadFile(opened)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Fatalf("file content = %q", data)
	}
}

func TestFileSystemServiceCreateDuplicateAddsTimestamp(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFileSystemService(storage.NewOSFileSystem(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create("file.txt", []byte("one")); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Create("file.txt", []byte("two"))
	if err != nil {
		t.Fatalf("Create duplicate error = %v", err)
	}
	if resp.Filename == "file.txt" {
		t.Fatal("duplicate filename was not changed")
	}
	if !strings.HasPrefix(resp.Filename, "file_") || !strings.HasSuffix(resp.Filename, ".txt") {
		t.Fatalf("unexpected duplicate filename %q", resp.Filename)
	}
}

func TestFileSystemServiceInvalidAndMissing(t *testing.T) {
	root := t.TempDir()
	svc, err := NewFileSystemService(storage.NewOSFileSystem(root))
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{"", "../x", "dir/file", `bad\file`, "bad:file"}
	for _, name := range invalid {
		if _, err := svc.Create(name, []byte("x")); !errors.Is(err, ErrInvalidFilename) {
			t.Fatalf("Create(%q) error = %v, want invalid filename", name, err)
		}
		if _, err := svc.Open(name); !errors.Is(err, ErrInvalidFilename) {
			t.Fatalf("Open(%q) error = %v, want invalid filename", name, err)
		}
	}
	if _, err := svc.Open("missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open missing error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "attachments", "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Open("folder"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open directory error = %v", err)
	}
}
