package gotstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// Refs manages references (branches/tags) for a bare repository on the local filesystem.
type Refs struct {
	dir string // e.g. "/data/repos/1/refs"
}

func NewRefs(dir string) *Refs {
	return &Refs{dir: dir}
}

// Get returns the commit hash for a reference (e.g. "heads/main").
func (r *Refs) Get(name string) (object.Hash, error) {
	data, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(name)))
	if err != nil {
		return "", fmt.Errorf("ref %s: %w", name, err)
	}
	return object.Hash(strings.TrimSpace(string(data))), nil
}

// Set updates a reference to point to a commit hash.
func (r *Refs) Set(name string, h object.Hash) error {
	path := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(string(h)+"\n"), 0o644)
}

// Delete removes a reference.
func (r *Refs) Delete(name string) error {
	return os.Remove(filepath.Join(r.dir, filepath.FromSlash(name)))
}

// List returns all references under a prefix (e.g. "heads/" for branches).
func (r *Refs) List(prefix string) (map[string]object.Hash, error) {
	dir := filepath.Join(r.dir, filepath.FromSlash(prefix))
	refs := make(map[string]object.Hash)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(r.dir, path)
		if err != nil {
			return err
		}
		name := strings.ReplaceAll(rel, string(filepath.Separator), "/")
		h, err := r.Get(name)
		if err != nil {
			return nil
		}
		refs[name] = h
		return nil
	})
	if os.IsNotExist(err) {
		return refs, nil
	}
	return refs, err
}

// ListAll returns all references.
func (r *Refs) ListAll() (map[string]object.Hash, error) {
	return r.List("")
}

// HEAD returns the default branch commit hash.
func (r *Refs) HEAD(defaultBranch string) (object.Hash, error) {
	return r.Get("heads/" + defaultBranch)
}
