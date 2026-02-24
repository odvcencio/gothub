package storage

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalBackend stores objects on the local filesystem.
type LocalBackend struct {
	root string
}

func NewLocalBackend(root string) (*LocalBackend, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalBackend{root: root}, nil
}

func (l *LocalBackend) path(p string) string {
	return filepath.Join(l.root, filepath.FromSlash(p))
}

func (l *LocalBackend) Read(path string) (io.ReadCloser, error) {
	return os.Open(l.path(path))
}

func (l *LocalBackend) Write(path string, data []byte) error {
	full := l.path(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

func (l *LocalBackend) Has(path string) (bool, error) {
	_, err := os.Stat(l.path(path))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (l *LocalBackend) Delete(path string) error {
	return os.Remove(l.path(path))
}

func (l *LocalBackend) List(prefix string) ([]string, error) {
	dir := l.path(prefix)
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(l.root, p)
		if err != nil {
			return err
		}
		paths = append(paths, strings.ReplaceAll(rel, string(filepath.Separator), "/"))
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return paths, err
}

var _ Backend = (*LocalBackend)(nil)
