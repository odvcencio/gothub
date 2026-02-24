package gotprotocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/gotstore"
)

const (
	maxPushBodyBytes   int64 = 64 << 20
	maxPushObjectBytes int   = 16 << 20
	maxPushObjectCount int   = 50000
	maxRefUpdateBytes  int64 = 4 << 20
	maxBatchRequestB   int64 = 2 << 20
	defaultBatchMaxObj int   = 10000
	maxBatchMaxObj     int   = 50000
)

// Handler provides HTTP endpoints for the Got protocol (push/pull).
type Handler struct {
	getStore    func(owner, repo string) (*gotstore.RepoStore, error)
	authorize   func(r *http.Request, owner, repo string, write bool) (int, error)
	indexCommit func(ctx context.Context, owner, repo string, commitHash object.Hash) error
}

func NewHandler(
	getStore func(owner, repo string) (*gotstore.RepoStore, error),
	authorize func(r *http.Request, owner, repo string, write bool) (int, error),
	indexCommit func(ctx context.Context, owner, repo string, commitHash object.Hash) error,
) *Handler {
	return &Handler{getStore: getStore, authorize: authorize, indexCommit: indexCommit}
}

// RegisterRoutes sets up Got protocol routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /got/{owner}/{repo}/refs", h.handleListRefs)
	mux.HandleFunc("POST /got/{owner}/{repo}/objects/batch", h.handleBatchObjects)
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

// POST /{owner}/{repo}.got/objects/batch — fetch missing object graph in one round-trip.
func (h *Handler) handleBatchObjects(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, false); !ok {
		return
	}
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var req struct {
		Wants      []string `json:"wants"`
		Haves      []string `json:"haves"`
		MaxObjects int      `json:"max_objects"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBatchRequestB)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if len(req.Wants) == 0 {
		http.Error(w, "at least one want hash is required", http.StatusBadRequest)
		return
	}

	maxObjects := req.MaxObjects
	if maxObjects <= 0 {
		maxObjects = defaultBatchMaxObj
	}
	if maxObjects > maxBatchMaxObj {
		maxObjects = maxBatchMaxObj
	}

	haveSet := make(map[object.Hash]bool, len(req.Haves))
	for _, h := range req.Haves {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		haveSet[object.Hash(h)] = true
	}

	seen := make(map[object.Hash]bool)
	missing := make([]object.Hash, 0, maxObjects)
	truncated := false
	for _, want := range req.Wants {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		root := object.Hash(want)
		if !store.Objects.Has(root) {
			continue
		}
		objs, err := WalkObjects(store.Objects, root, func(h object.Hash) bool {
			return haveSet[h] || seen[h]
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("walk objects for %s: %v", root, err), http.StatusBadRequest)
			return
		}
		for _, h := range objs {
			if seen[h] {
				continue
			}
			seen[h] = true
			missing = append(missing, h)
			if len(missing) >= maxObjects {
				truncated = true
				break
			}
		}
		if truncated {
			break
		}
	}

	type batchObject struct {
		Hash string `json:"hash"`
		Type string `json:"type"`
		Data []byte `json:"data"`
	}
	out := make([]batchObject, 0, len(missing))
	for _, h := range missing {
		objType, data, err := store.Objects.Read(h)
		if err != nil {
			http.Error(w, fmt.Sprintf("read object %s: %v", h, err), http.StatusInternalServerError)
			return
		}
		out = append(out, batchObject{
			Hash: string(h),
			Type: string(objType),
			Data: data,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"objects":   out,
		"truncated": truncated,
	})
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

	type pushedObject struct {
		Type string `json:"type"`
		Data []byte `json:"data"`
	}
	type decodedPushObject struct {
		objType object.ObjectType
		data    []byte
		hash    object.Hash
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPushBodyBytes))
	decoded := make([]decodedPushObject, 0, 128)
	known := make(map[object.Hash]object.ObjectType)
	for {
		var obj pushedObject
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			http.Error(w, fmt.Sprintf("decode object %d: %v", len(decoded), err), http.StatusBadRequest)
			return
		}
		if len(decoded) >= maxPushObjectCount {
			http.Error(w, "too many objects in push", http.StatusRequestEntityTooLarge)
			return
		}
		objType, err := parsePushedObjectType(obj.Type)
		if err != nil {
			http.Error(w, fmt.Sprintf("object %d: %v", len(decoded), err), http.StatusBadRequest)
			return
		}
		if len(obj.Data) > maxPushObjectBytes {
			http.Error(w, fmt.Sprintf("object %d exceeds %d-byte limit", len(decoded), maxPushObjectBytes), http.StatusRequestEntityTooLarge)
			return
		}
		hash := object.HashObject(objType, obj.Data)
		decoded = append(decoded, decodedPushObject{
			objType: objType,
			data:    obj.Data,
			hash:    hash,
		})
		known[hash] = objType
	}

	for i, obj := range decoded {
		if err := validatePushedObject(obj.objType, obj.data, known, store.Objects); err != nil {
			http.Error(w, fmt.Sprintf("object %d validation failed: %v", i, err), http.StatusBadRequest)
			return
		}
	}
	for i, obj := range decoded {
		if _, err := store.Objects.Write(obj.objType, obj.data); err != nil {
			http.Error(w, fmt.Sprintf("write object %d: %v", i, err), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"received": len(decoded)})
}

// POST /{owner}/{repo}.got/refs — update refs
func (h *Handler) handleUpdateRefs(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, true); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	var updates map[string]string
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRefUpdateBytes)).Decode(&updates); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	applied := make(map[string]string, len(updates))
	for name, hash := range updates {
		if hash == "" {
			if err := store.Refs.Delete(name); err != nil {
				http.Error(w, fmt.Sprintf("delete ref %s: %v", name, err), http.StatusInternalServerError)
				return
			}
		} else {
			target := object.Hash(hash)
			objType, _, err := store.Objects.Read(target)
			if err != nil {
				http.Error(w, fmt.Sprintf("set ref %s: target object missing: %v", name, err), http.StatusBadRequest)
				return
			}
			if objType != object.TypeCommit {
				http.Error(w, fmt.Sprintf("set ref %s: target must be commit, got %s", name, objType), http.StatusBadRequest)
				return
			}
			enrichedHash, err := ensureCommitEntities(store, target)
			if err != nil {
				http.Error(w, fmt.Sprintf("set ref %s: entity extraction failed: %v", name, err), http.StatusInternalServerError)
				return
			}
			if h.indexCommit != nil {
				if err := h.indexCommit(r.Context(), owner, repo, enrichedHash); err != nil {
					http.Error(w, fmt.Sprintf("set ref %s: lineage indexing failed: %v", name, err), http.StatusInternalServerError)
					return
				}
			}
			if err := store.Refs.Set(name, enrichedHash); err != nil {
				http.Error(w, fmt.Sprintf("set ref %s: %v", name, err), http.StatusInternalServerError)
				return
			}
			applied[name] = string(enrichedHash)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "updated": applied})
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

func parsePushedObjectType(raw string) (object.ObjectType, error) {
	switch object.ObjectType(strings.TrimSpace(raw)) {
	case object.TypeBlob, object.TypeTree, object.TypeCommit, object.TypeEntity, object.TypeEntityList:
		return object.ObjectType(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("unsupported object type %q", raw)
	}
}

func resolveObjectType(hash object.Hash, known map[object.Hash]object.ObjectType, store *object.Store) (object.ObjectType, bool) {
	if t, ok := known[hash]; ok {
		return t, true
	}
	if !store.Has(hash) {
		return "", false
	}
	t, _, err := store.Read(hash)
	if err != nil {
		return "", false
	}
	return t, true
}

func validatePushedObject(objType object.ObjectType, data []byte, known map[object.Hash]object.ObjectType, store *object.Store) error {
	requireRef := func(hash object.Hash, expected ...object.ObjectType) error {
		if hash == "" {
			return fmt.Errorf("empty reference hash")
		}
		gotType, ok := resolveObjectType(hash, known, store)
		if !ok {
			return fmt.Errorf("missing referenced object %s", hash)
		}
		if len(expected) == 0 {
			return nil
		}
		for _, t := range expected {
			if gotType == t {
				return nil
			}
		}
		return fmt.Errorf("referenced object %s has type %s", hash, gotType)
	}

	switch objType {
	case object.TypeBlob:
		_, err := object.UnmarshalBlob(data)
		return err
	case object.TypeEntity:
		_, err := object.UnmarshalEntity(data)
		return err
	case object.TypeEntityList:
		el, err := object.UnmarshalEntityList(data)
		if err != nil {
			return err
		}
		for _, ref := range el.EntityRefs {
			if err := requireRef(ref, object.TypeEntity); err != nil {
				return err
			}
		}
		return nil
	case object.TypeTree:
		tree, err := object.UnmarshalTree(data)
		if err != nil {
			return err
		}
		for _, e := range tree.Entries {
			if strings.TrimSpace(e.Name) == "" {
				return fmt.Errorf("tree entry has empty name")
			}
			if strings.Contains(e.Name, "/") {
				return fmt.Errorf("tree entry %q contains path separator", e.Name)
			}
			if e.IsDir {
				if err := requireRef(e.SubtreeHash, object.TypeTree); err != nil {
					return fmt.Errorf("tree entry %q subtree: %w", e.Name, err)
				}
			} else {
				if err := requireRef(e.BlobHash, object.TypeBlob); err != nil {
					return fmt.Errorf("tree entry %q blob: %w", e.Name, err)
				}
				if e.EntityListHash != "" {
					if err := requireRef(e.EntityListHash, object.TypeEntityList); err != nil {
						return fmt.Errorf("tree entry %q entity list: %w", e.Name, err)
					}
				}
			}
		}
		return nil
	case object.TypeCommit:
		commit, err := object.UnmarshalCommit(data)
		if err != nil {
			return err
		}
		if err := requireRef(commit.TreeHash, object.TypeTree); err != nil {
			return fmt.Errorf("commit tree: %w", err)
		}
		for _, p := range commit.Parents {
			if err := requireRef(p, object.TypeCommit); err != nil {
				return fmt.Errorf("commit parent: %w", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported object type %q", objType)
	}
}

func ensureCommitEntities(store *gotstore.RepoStore, commitHash object.Hash) (object.Hash, error) {
	commit, err := store.Objects.ReadCommit(commitHash)
	if err != nil {
		return "", err
	}
	newTreeHash, changed, err := rewriteTreeWithEntities(store, commit.TreeHash, "")
	if err != nil {
		return "", err
	}
	if !changed {
		return commitHash, nil
	}
	updated := *commit
	updated.TreeHash = newTreeHash
	return store.Objects.WriteCommit(&updated)
}

func rewriteTreeWithEntities(store *gotstore.RepoStore, treeHash object.Hash, prefix string) (object.Hash, bool, error) {
	tree, err := store.Objects.ReadTree(treeHash)
	if err != nil {
		return "", false, err
	}
	changed := false
	updated := make([]object.TreeEntry, len(tree.Entries))
	for i, e := range tree.Entries {
		entry := e
		fullPath := e.Name
		if prefix != "" {
			fullPath = prefix + "/" + e.Name
		}
		if e.IsDir {
			newSubtreeHash, subtreeChanged, err := rewriteTreeWithEntities(store, e.SubtreeHash, fullPath)
			if err != nil {
				return "", false, err
			}
			if subtreeChanged {
				entry.SubtreeHash = newSubtreeHash
				changed = true
			}
		} else if e.EntityListHash == "" {
			blob, err := store.Objects.ReadBlob(e.BlobHash)
			if err != nil {
				return "", false, err
			}
			el, err := entity.Extract(fullPath, blob.Data)
			if err == nil && len(el.Entities) > 0 {
				entityRefs := make([]object.Hash, 0, len(el.Entities))
				for _, ent := range el.Entities {
					entHash, err := store.Objects.WriteEntity(&object.EntityObj{
						Kind:     entityKindToString(ent.Kind),
						Name:     ent.Name,
						DeclKind: ent.DeclKind,
						Receiver: ent.Receiver,
						Body:     ent.Body,
						BodyHash: object.Hash(ent.BodyHash),
					})
					if err != nil {
						return "", false, err
					}
					entityRefs = append(entityRefs, entHash)
				}
				entityListHash, err := store.Objects.WriteEntityList(&object.EntityListObj{
					Language:   el.Language,
					Path:       fullPath,
					EntityRefs: entityRefs,
				})
				if err != nil {
					return "", false, err
				}
				entry.EntityListHash = entityListHash
				changed = true
			}
		}
		updated[i] = entry
	}
	if !changed {
		return treeHash, false, nil
	}
	newTreeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: updated})
	if err != nil {
		return "", false, err
	}
	return newTreeHash, true, nil
}

func entityKindToString(k entity.EntityKind) string {
	switch k {
	case entity.KindPreamble:
		return "preamble"
	case entity.KindImportBlock:
		return "import"
	case entity.KindDeclaration:
		return "declaration"
	case entity.KindInterstitial:
		return "interstitial"
	default:
		return "unknown"
	}
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
		case object.TypeEntityList:
			el, err := store.ReadEntityList(h)
			if err != nil {
				return err
			}
			for _, ref := range el.EntityRefs {
				if err := walk(ref); err != nil {
					return err
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
