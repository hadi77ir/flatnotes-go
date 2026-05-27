package storage

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type FileSystem interface {
	Open(name string) (fs.File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	CreateExclusive(name string, perm fs.FileMode) (io.WriteCloser, error)
	Remove(name string) error
	Rename(oldName, newName string) error
	MkdirAll(name string, perm fs.FileMode) error
	Stat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Root() string
}

type OSFileSystem struct {
	root string
}

func NewOSFileSystem(root string) *OSFileSystem {
	return &OSFileSystem{root: root}
}

func (f *OSFileSystem) Root() string {
	return f.root
}

func (f *OSFileSystem) Open(name string) (fs.File, error) {
	return os.Open(f.path(name))
}

func (f *OSFileSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(f.path(name))
}

func (f *OSFileSystem) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(f.path(name), data, perm)
}

func (f *OSFileSystem) CreateExclusive(name string, perm fs.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(f.path(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

func (f *OSFileSystem) Remove(name string) error {
	return os.Remove(f.path(name))
}

func (f *OSFileSystem) Rename(oldName, newName string) error {
	return os.Rename(f.path(oldName), f.path(newName))
}

func (f *OSFileSystem) MkdirAll(name string, perm fs.FileMode) error {
	return os.MkdirAll(f.path(name), perm)
}

func (f *OSFileSystem) Stat(name string) (fs.FileInfo, error) {
	return os.Stat(f.path(name))
}

func (f *OSFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(f.path(name))
}

func (f *OSFileSystem) CopyIntoExclusive(name string, r io.Reader, perm fs.FileMode) error {
	file, err := f.CreateExclusive(name, perm)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, r)
	return err
}

func (f *OSFileSystem) path(name string) string {
	if name == "" || name == "." {
		return f.root
	}
	return filepath.Join(f.root, filepath.Clean(name))
}
