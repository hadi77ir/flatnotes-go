package attachments

import (
	"errors"
	"io/fs"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hadi77ir/flatnotes-go/internal/storage"
)

var invalidFilenameChars = regexp.MustCompile(`[<>:"/\\|?*]`)

type FileSystemService struct {
	fs storage.FileSystem
}

func NewFileSystemService(fileSystem storage.FileSystem) (*FileSystemService, error) {
	if err := fileSystem.MkdirAll("attachments", 0o755); err != nil {
		return nil, err
	}
	return &FileSystemService{fs: fileSystem}, nil
}

func (s *FileSystemService) Create(filename string, data []byte) (CreateResponse, error) {
	filename = strings.TrimSpace(filename)
	if !validFilename(filename) {
		return CreateResponse{}, ErrInvalidFilename
	}
	saved := filename
	if err := s.write(saved, data); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return CreateResponse{}, err
		}
		saved = withTimestamp(filename)
		if err := s.write(saved, data); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return CreateResponse{}, ErrExists
			}
			return CreateResponse{}, err
		}
	}
	return CreateResponse{Filename: saved, URL: "attachments/" + url.PathEscape(saved)}, nil
}

func (s *FileSystemService) Open(filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if !validFilename(filename) {
		return "", ErrInvalidFilename
	}
	path := filepath.Join("attachments", filename)
	info, err := s.fs.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	if info.IsDir() {
		return "", ErrNotFound
	}
	return filepath.Join(s.fs.Root(), path), nil
}

func (s *FileSystemService) write(filename string, data []byte) error {
	file, err := s.fs.CreateExclusive(filepath.Join("attachments", filename), 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func validFilename(value string) bool {
	return value != "" && !invalidFilenameChars.MatchString(value)
}

func withTimestamp(filename string) string {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	return name + "_" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + ext
}
