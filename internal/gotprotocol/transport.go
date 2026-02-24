package gotprotocol

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
)

// Handler provides HTTP endpoints for the Got protocol (push/pull).
type Handler struct {
	getStore  func(owner, repo string) (*gotstore.RepoStore, error)
	authorize func(r *http.Request, owner, repo string, write bool) (int, error)
}

func NewHandler(
	getStore func(owner, repo string) (*gotstore.RepoStore, error),
	authorize func(r *http.Request, owner, repo string, write bool) (int, error),
) *Handler {
	return &Handler{getStore: getStore, authorize: authorize}
}

// RegisterRoutes sets up Got protocol routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /got/{owner}/{repo}/refs", h.handleListRefs)
	mux.HandleFunc("GET /got/{owner}/{repo}/objects/{hash}", h.handleGetObject)
	mux.HandleFunc("POST /got/{owner}/{repo}/objects", h.handlePushObjects)
	mux.HandleFunc("POST /got/{owner}/{repo}/refs", h.handleUpdateRefs)
}

// GET /{owner}/{repo}.got/refs — list all refs
func (h *Handler) handleListRefs(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, false); !ok {
		return
	}
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	refs, err := store.Refs.ListAll()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(refs)
}

// GET /{owner}/{repo}.got/objects/{hash} — fetch a single object
func (h *Handler) handleGetObject(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, false); !ok {
		return
	}
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	hash := object.Hash(r.PathValue("hash"))
	if !store.Objects.Has(hash) {
		http.Error(w, "object not found", http.StatusNotFound)
		return
	}
	objType, data, err := store.Objects.Read(hash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Object-Type", string(objType))
	w.Write(data)
}

// POST /{owner}/{repo}.got/objects — push objects (newline-delimited JSON)
func (h *Handler) handlePushObjects(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, true); !ok {
		return
	}
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	dec := json.NewDecoder(r.Body)
	var count int
	for dec.More() {
		var obj struct {
			Type string `json:"type"`
			Data []byte `json:"data"`
		}
		if err := dec.Decode(&obj); err != nil {
			http.Error(w, fmt.Sprintf("decode object %d: %v", count, err), http.StatusBadRequest)
			return
		}
		if _, err := store.Objects.Write(object.ObjectType(obj.Type), obj.Data); err != nil {
			http.Error(w, fmt.Sprintf("write object %d: %v", count, err), http.StatusInternalServerError)
			return
		}
		count++
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"received": count})
}

// POST /{owner}/{repo}.got/refs — update refs
func (h *Handler) handleUpdateRefs(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, true); !ok {
		return
	}
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var updates map[string]string
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	for name, hash := range updates {
		if hash == "" {
			if err := store.Refs.Delete(name); err != nil {
				http.Error(w, fmt.Sprintf("delete ref %s: %v", name, err), http.StatusInternalServerError)
				return
			}
		} else {
			if err := store.Refs.Set(name, object.Hash(hash)); err != nil {
				http.Error(w, fmt.Sprintf("set ref %s: %v", name, err), http.StatusInternalServerError)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) repoStore(r *http.Request) (*gotstore.RepoStore, error) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	return h.getStore(owner, repo)
}

func (h *Handler) authorizeRequest(w http.ResponseWriter, r *http.Request, write bool) bool {
	if h.authorize == nil {
		return true
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	status, err := h.authorize(r, owner, repo, write)
	if err == nil {
		return true
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="gothub"`)
	}
	http.Error(w, err.Error(), status)
	return false
}

// WalkObjects walks the object graph from a commit hash, calling fn for each
// object that the remote doesn't have (determined by the has function).
func WalkObjects(store *object.Store, root object.Hash, has func(object.Hash) bool) ([]object.Hash, error) {
	var missing []object.Hash
	seen := make(map[object.Hash]bool)

	var walk func(h object.Hash) error
	walk = func(h object.Hash) error {
		if seen[h] || has(h) {
			return nil
		}
		seen[h] = true

		objType, _, err := store.Read(h)
		if err != nil {
			return err
		}
		missing = append(missing, h)

		switch objType {
		case object.TypeCommit:
			commit, err := store.ReadCommit(h)
			if err != nil {
				return err
			}
			if err := walk(commit.TreeHash); err != nil {
				return err
			}
			for _, p := range commit.Parents {
				if err := walk(p); err != nil {
					return err
				}
			}
		case object.TypeTree:
			tree, err := store.ReadTree(h)
			if err != nil {
				return err
			}
			for _, e := range tree.Entries {
				if e.IsDir {
					if err := walk(e.SubtreeHash); err != nil {
						return err
					}
				} else {
					if err := walk(e.BlobHash); err != nil {
						return err
					}
					if e.EntityListHash != "" {
						if err := walk(e.EntityListHash); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}

	if err := walk(root); err != nil {
		return nil, err
	}
	return missing, nil
}
