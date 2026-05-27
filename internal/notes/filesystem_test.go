package notes

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hadi77ir/flatnotes-go/internal/storage"
)

func newTestNotes(t *testing.T) (*FileSystemService, string) {
	t.Helper()
	root := t.TempDir()
	svc, err := NewFileSystemService(storage.NewOSFileSystem(root), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewFileSystemService() error = %v", err)
	}
	t.Cleanup(func() {
		if err := svc.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return svc, root
}

func TestFileSystemNotesCRUD(t *testing.T) {
	svc, root := newTestNotes(t)
	content := "hello #Tag"
	note, err := svc.Create(CreateRequest{Title: " First ", Content: &content})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if note.Title != "First" || note.Content == nil || *note.Content != content {
		t.Fatalf("created note = %+v", note)
	}
	if _, err := os.Stat(filepath.Join(root, "First.md")); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get("First")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Content == nil || *got.Content != content {
		t.Fatalf("Get content = %+v", got.Content)
	}
	newTitle := "Second"
	newContent := "updated #Other"
	updated, err := svc.Update("First", UpdateRequest{NewTitle: &newTitle, NewContent: &newContent})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Title != "Second" || updated.Content == nil || *updated.Content != newContent {
		t.Fatalf("updated note = %+v", updated)
	}
	if err := svc.Delete("Second"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := svc.Get("Second"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get deleted error = %v", err)
	}
}

func TestFileSystemNotesInvalidMissingAndConflict(t *testing.T) {
	svc, _ := newTestNotes(t)
	content := "x"
	invalidTitles := []string{"", "bad/name", `bad\name`, "bad:name"}
	for _, title := range invalidTitles {
		if _, err := svc.Create(CreateRequest{Title: title, Content: &content}); !errors.Is(err, ErrInvalidTitle) {
			t.Fatalf("Create(%q) error = %v", title, err)
		}
		if _, err := svc.Get(title); !errors.Is(err, ErrInvalidTitle) {
			t.Fatalf("Get(%q) error = %v", title, err)
		}
	}
	if _, err := svc.Get("Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error = %v", err)
	}
	if _, err := svc.Update("Missing", UpdateRequest{NewContent: &content}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update missing error = %v", err)
	}
	if err := svc.Delete("Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing error = %v", err)
	}
	if _, err := svc.Create(CreateRequest{Title: "A", Content: &content}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateRequest{Title: "A", Content: &content}); !errors.Is(err, ErrExists) {
		t.Fatalf("Create duplicate error = %v", err)
	}
	if _, err := svc.Create(CreateRequest{Title: "B", Content: &content}); err != nil {
		t.Fatal(err)
	}
	titleB := "B"
	if _, err := svc.Update("A", UpdateRequest{NewTitle: &titleB}); !errors.Is(err, ErrExists) {
		t.Fatalf("Update duplicate title error = %v", err)
	}
}

func TestFileSystemNotesSearchTagsAndExternalSync(t *testing.T) {
	svc, root := newTestNotes(t)
	alpha := "alpha body #Project `#ignored`"
	beta := "beta body #Project #Other"
	if _, err := svc.Create(CreateRequest{Title: "Alpha", Content: &alpha}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(CreateRequest{Title: "Beta", Content: &beta}); err != nil {
		t.Fatal(err)
	}
	results, err := svc.Search("#project", "title", "asc", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 || results[0].Title != "Alpha" || results[1].Title != "Beta" {
		t.Fatalf("Search results = %+v", results)
	}
	if results[0].Score != nil {
		t.Fatalf("score should be nil when sorting by title: %+v", results[0].Score)
	}
	tags, err := svc.GetTags()
	if err != nil {
		t.Fatalf("GetTags() error = %v", err)
	}
	wantTags := []string{"other", "project"}
	if len(tags) != len(wantTags) || tags[0] != wantTags[0] || tags[1] != wantTags[1] {
		t.Fatalf("tags = %+v", tags)
	}
	if err := os.WriteFile(filepath.Join(root, "External.md"), []byte("external #newtag"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err = svc.Search("external", "score", "desc", 10)
	if err != nil {
		t.Fatalf("Search external error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "External" {
		t.Fatalf("external search results = %+v", results)
	}
}
