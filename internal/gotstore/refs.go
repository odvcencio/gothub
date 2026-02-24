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

// RefCASMismatchError indicates an atomic ref update failed due to compare-and-swap mismatch.
type RefCASMismatchError struct {
	Name     string
	Expected object.Hash
	Actual   object.Hash
}

func (e *RefCASMismatchError) Error() string {
	return fmt.Sprintf("stale ref %s (expected %s, got %s)", e.Name, e.Expected, e.Actual)
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
	return r.Update(name, nil, &h)
}

// Update atomically compares and updates/deletes a reference.
// If expectedOld is non-nil and doesn't match the current value, returns RefCASMismatchError.
// If newHash is nil or empty, the ref is deleted.
func (r *Refs) Update(name string, expectedOld *object.Hash, newHash *object.Hash) error {
	path := filepath.Join(r.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("acquire ref lock %s: %w", lockPath, err)
	}
	current, err := r.getNoErrNotExist(name)
	if err != nil {
		f.Close()
		os.Remove(lockPath)
		return err
	}
	if expectedOld != nil && current != *expectedOld {
		f.Close()
		os.Remove(lockPath)
		return &RefCASMismatchError{
			Name:     name,
			Expected: *expectedOld,
			Actual:   current,
		}
	}
	if newHash == nil || *newHash == "" {
		if err := f.Close(); err != nil {
			os.Remove(lockPath)
			return fmt.Errorf("close ref lock %s: %w", lockPath, err)
		}
		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove ref lock %s: %w", lockPath, err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete ref %s: %w", name, err)
		}
		return nil
	}
	if _, err := f.WriteString(string(*newHash) + "\n"); err != nil {
		f.Close()
		os.Remove(lockPath)
		return fmt.Errorf("write ref lock %s: %w", lockPath, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(lockPath)
		return fmt.Errorf("close ref lock %s: %w", lockPath, err)
	}
	if err := os.Rename(lockPath, path); err != nil {
		os.Remove(lockPath)
		return fmt.Errorf("rename ref lock %s: %w", lockPath, err)
	}
	return nil
}

// Delete removes a reference.
func (r *Refs) Delete(name string) error {
	return r.Update(name, nil, nil)
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

func (r *Refs) getNoErrNotExist(name string) (object.Hash, error) {
	h, err := r.Get(name)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(strings.ToLower(err.Error()), "no such file") {
			return "", nil
		}
		return "", err
	}
	return h, nil
}
