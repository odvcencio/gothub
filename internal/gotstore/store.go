package gotstore

import (
	"os"
	"path/filepath"

	"github.com/odvcencio/got/pkg/object"
)

// RepoStore provides access to a bare repository's object store and refs.
// Each repository gets its own RepoStore rooted at its storage path.
type RepoStore struct {
	Objects *object.Store
	Refs    *Refs
	root    string
}

// Open opens (or creates) a bare repository store at the given path.
func Open(repoPath string) (*RepoStore, error) {
	objDir := filepath.Join(repoPath, "objects")
	refsDir := filepath.Join(repoPath, "refs")
	for _, d := range []string{objDir, refsDir, filepath.Join(refsDir, "heads"), filepath.Join(refsDir, "tags")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &RepoStore{
		Objects: object.NewStore(objDir),
		Refs:    NewRefs(refsDir),
		root:    repoPath,
	}, nil
}

// Root returns the bare repository root path.
func (rs *RepoStore) Root() string { return rs.root }
