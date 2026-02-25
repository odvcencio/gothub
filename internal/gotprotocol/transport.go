package gotprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/entityutil"
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

	gotProtocolHeader     = "Got-Protocol"
	gotCapabilitiesHeader = "Got-Capabilities"
	gotLimitsHeader       = "Got-Limits"

	serverProtocolVersion = "1"
	serverCapabilities    = "pack,zstd,sideband"
)

// serverLimitsValue is the Got-Limits header value advertised by this server.
var serverLimitsValue = fmt.Sprintf(
	"max_batch=%d,max_payload=%d,max_object=%d",
	maxBatchMaxObj, maxPushBodyBytes, maxPushObjectBytes,
)

// Handler provides HTTP endpoints for the Got protocol (push/pull).
type Handler struct {
	getStore    func(owner, repo string) (*gotstore.RepoStore, error)
	authorize   func(r *http.Request, owner, repo string, write bool) (int, error)
	indexCommit func(ctx context.Context, owner, repo string, commitHash object.Hash) error
	validateRef func(ctx context.Context, owner, repo, refName string, oldHash, newHash object.Hash) error
}

type refUpdateRequest struct {
	Updates []refUpdateItem `json:"updates"`
}

type refUpdateItem struct {
	Name string  `json:"name"`
	Old  *string `json:"old,omitempty"`
	New  *string `json:"new"`
}

func NewHandler(
	getStore func(owner, repo string) (*gotstore.RepoStore, error),
	authorize func(r *http.Request, owner, repo string, write bool) (int, error),
	indexCommit func(ctx context.Context, owner, repo string, commitHash object.Hash) error,
) *Handler {
	return &Handler{getStore: getStore, authorize: authorize, indexCommit: indexCommit}
}

func (h *Handler) SetRefUpdateValidator(fn func(ctx context.Context, owner, repo, refName string, oldHash, newHash object.Hash) error) {
	h.validateRef = fn
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
	h.setProtocolHeaders(w)
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

	limitStr := r.URL.Query().Get("limit")
	cursor := r.URL.Query().Get("cursor")

	// If no pagination params, return legacy flat format
	if limitStr == "" && cursor == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refs)
		return
	}

	// Parse limit (default 1000)
	limit := 1000
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 10000 {
		limit = 10000
	}

	// Sort ref names for deterministic pagination
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)

	// Find cursor position
	startIdx := 0
	if cursor != "" {
		for i, name := range names {
			if name > cursor {
				startIdx = i
				break
			}
			if i == len(names)-1 {
				// cursor past all refs
				startIdx = len(names)
			}
		}
	}

	// Build page
	pageRefs := make(map[string]object.Hash)
	endIdx := startIdx + limit
	if endIdx > len(names) {
		endIdx = len(names)
	}
	for _, name := range names[startIdx:endIdx] {
		pageRefs[name] = refs[name]
	}

	resp := map[string]any{"refs": pageRefs}
	if endIdx < len(names) {
		resp["cursor"] = names[endIdx-1]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// GET /{owner}/{repo}.got/objects/{hash} — fetch a single object
func (h *Handler) handleGetObject(w http.ResponseWriter, r *http.Request) {
	if ok := h.authorizeRequest(w, r, false); !ok {
		return
	}
	h.setProtocolHeaders(w)
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	hash := object.Hash(r.PathValue("hash"))
	if !store.Objects.Has(hash) {
		writeJSONError(w, "object_not_found", "object not found", string(hash), http.StatusNotFound)
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
	h.setProtocolHeaders(w)
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
		writeJSONError(w, "invalid_request", "at least one want hash is required", "", http.StatusBadRequest)
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

	// Check if client accepts pack transport
	acceptPack := strings.Contains(r.Header.Get("Accept"), "application/x-got-pack")

	if acceptPack {
		// Encode as pack stream
		var packBuf bytes.Buffer
		pw, err := object.NewPackWriter(&packBuf, uint32(len(missing)))
		if err != nil {
			http.Error(w, fmt.Sprintf("create pack writer: %v", err), http.StatusInternalServerError)
			return
		}

		var entityEntries []object.PackEntityTrailerEntry
		for _, h := range missing {
			objType, data, err := store.Objects.Read(h)
			if err != nil {
				http.Error(w, fmt.Sprintf("read object %s: %v", h, err), http.StatusInternalServerError)
				return
			}
			packType := objectTypeToPackType(objType)
			if objType == object.TypeEntity || objType == object.TypeEntityList {
				entityEntries = append(entityEntries, object.PackEntityTrailerEntry{
					ObjectHash: h,
					StableID:   "type:" + string(objType),
				})
			}
			if err := pw.WriteEntry(packType, data); err != nil {
				http.Error(w, fmt.Sprintf("write pack entry: %v", err), http.StatusInternalServerError)
				return
			}
		}

		if len(entityEntries) > 0 {
			if _, err := pw.FinishWithEntityTrailer(entityEntries); err != nil {
				http.Error(w, fmt.Sprintf("finish pack: %v", err), http.StatusInternalServerError)
				return
			}
		} else {
			if _, err := pw.Finish(); err != nil {
				http.Error(w, fmt.Sprintf("finish pack: %v", err), http.StatusInternalServerError)
				return
			}
		}

		packData := packBuf.Bytes()

		// Compress with zstd if client accepts it
		if strings.Contains(r.Header.Get("Accept-Encoding"), "zstd") {
			enc, err := zstd.NewWriter(nil)
			if err != nil {
				http.Error(w, "zstd init: "+err.Error(), http.StatusInternalServerError)
				return
			}
			packData = enc.EncodeAll(packData, nil)
			enc.Close()
			w.Header().Set("Content-Encoding", "zstd")
		}

		if truncated {
			w.Header().Set("X-Truncated", "true")
		}
		w.Header().Set("Content-Type", "application/x-got-pack")
		w.Write(packData)
		return
	}

	// Existing JSON response path
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
	h.setProtocolHeaders(w)
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	type pushedObject struct {
		Hash string `json:"hash,omitempty"`
		Type string `json:"type"`
		Data []byte `json:"data"`
	}
	type decodedPushObject struct {
		objType object.ObjectType
		data    []byte
		hash    object.Hash
	}

	var decoded []decodedPushObject
	known := make(map[object.Hash]object.ObjectType)
	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "application/x-got-pack") {
		// Pack transport: read body, optionally decompress zstd, decode pack
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPushBodyBytes))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		if strings.Contains(r.Header.Get("Content-Encoding"), "zstd") {
			dec, err := zstd.NewReader(nil)
			if err != nil {
				http.Error(w, "zstd init: "+err.Error(), http.StatusInternalServerError)
				return
			}
			body, err = dec.DecodeAll(body, nil)
			dec.Close()
			if err != nil {
				http.Error(w, "decompress: "+err.Error(), http.StatusBadRequest)
				return
			}
		}

		pf, err := object.ReadPack(body)
		if err != nil {
			http.Error(w, "read pack: "+err.Error(), http.StatusBadRequest)
			return
		}
		resolved, err := object.ResolvePackEntries(pf.Entries)
		if err != nil {
			http.Error(w, "resolve pack: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Build entity type overrides from trailer
		typeOverrides := map[object.Hash]object.ObjectType{}
		if pf.EntityTrailer != nil {
			for _, entry := range pf.EntityTrailer.Entries {
				if len(entry.StableID) > 5 && entry.StableID[:5] == "type:" {
					typeOverrides[entry.ObjectHash] = object.ObjectType(entry.StableID[5:])
				}
			}
		}

		if len(resolved) > maxPushObjectCount {
			writeJSONError(w, "payload_too_large", "too many objects in push", "", http.StatusRequestEntityTooLarge)
			return
		}

		decoded = make([]decodedPushObject, 0, len(resolved))
		for _, entry := range resolved {
			objType := packTypeToObjectType(entry.Type)
			hash := object.HashObject(objType, entry.Data)
			if override, ok := typeOverrides[hash]; ok {
				objType = override
				hash = object.HashObject(objType, entry.Data)
			}
			if len(entry.Data) > maxPushObjectBytes {
				http.Error(w, fmt.Sprintf("object exceeds %d-byte limit", maxPushObjectBytes), http.StatusRequestEntityTooLarge)
				return
			}
			decoded = append(decoded, decodedPushObject{objType: objType, data: entry.Data, hash: hash})
			known[hash] = objType
		}
	} else {
		// Existing NDJSON path
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPushBodyBytes))
		decoded = make([]decodedPushObject, 0, 128)
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
				writeJSONError(w, "payload_too_large", "too many objects in push", "", http.StatusRequestEntityTooLarge)
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
			if provided := strings.TrimSpace(obj.Hash); provided != "" {
				if object.Hash(provided) != hash {
					writeJSONError(w, "hash_mismatch", "object hash mismatch", fmt.Sprintf("computed %s, got %s", hash, provided), http.StatusBadRequest)
					return
				}
			}
			decoded = append(decoded, decodedPushObject{
				objType: objType,
				data:    obj.Data,
				hash:    hash,
			})
			known[hash] = objType
		}
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
	h.setProtocolHeaders(w)
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	store, err := h.repoStore(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	updates, err := parseRefUpdates(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Precompute and validate all new target hashes before applying any ref updates.
	enrichedByTarget := make(map[object.Hash]object.Hash, len(updates))
	refTargets := make(map[string]object.Hash, len(updates))
	for _, u := range updates {
		if u.New == nil || *u.New == "" {
			continue
		}
		target := object.Hash(*u.New)
		objType, _, err := store.Objects.Read(target)
		if err != nil {
			http.Error(w, fmt.Sprintf("set ref %s: target object missing: %v", u.Name, err), http.StatusBadRequest)
			return
		}
		if objType != object.TypeCommit {
			http.Error(w, fmt.Sprintf("set ref %s: target must be commit, got %s", u.Name, objType), http.StatusBadRequest)
			return
		}
		enrichedHash, ok := enrichedByTarget[target]
		if !ok {
			enrichedHash, err = ensureCommitEntities(store, target)
			if err != nil {
				http.Error(w, fmt.Sprintf("set ref %s: entity extraction failed: %v", u.Name, err), http.StatusInternalServerError)
				return
			}
			if h.indexCommit != nil {
				if err := h.indexCommit(r.Context(), owner, repo, enrichedHash); err != nil {
					http.Error(w, fmt.Sprintf("set ref %s: lineage indexing failed: %v", u.Name, err), http.StatusInternalServerError)
					return
				}
			}
			enrichedByTarget[target] = enrichedHash
		}
		refTargets[u.Name] = enrichedHash
	}

	applied := make(map[string]string, len(updates))
	for _, u := range updates {
		currentHash, err := store.Refs.Get(u.Name)
		if err != nil {
			if !isMissingRefErr(err) {
				http.Error(w, fmt.Sprintf("read ref %s: %v", u.Name, err), http.StatusInternalServerError)
				return
			}
			currentHash = ""
		}
		newHash := object.Hash("")
		if u.New != nil && *u.New != "" {
			newHash = refTargets[u.Name]
		}
		if h.validateRef != nil {
			if err := h.validateRef(r.Context(), owner, repo, u.Name, currentHash, newHash); err != nil {
				http.Error(w, fmt.Sprintf("set ref %s: %v", u.Name, err), http.StatusConflict)
				return
			}
		}

		var expectedOld *object.Hash
		if u.Old != nil {
			oldHash := object.Hash(*u.Old)
			expectedOld = &oldHash
		}
		if u.New == nil || *u.New == "" {
			if err := store.Refs.Update(u.Name, expectedOld, nil); err != nil {
				var mismatch *gotstore.RefCASMismatchError
				if errors.As(err, &mismatch) {
					expected := ""
					if u.Old != nil {
						expected = *u.Old
					}
					writeJSONError(w, "ref_conflict", "stale old hash", fmt.Sprintf("expected %s, got %s", expected, mismatch.Actual), http.StatusConflict)
					return
				}
				http.Error(w, fmt.Sprintf("delete ref %s: %v", u.Name, err), http.StatusInternalServerError)
				return
			}
			applied[u.Name] = ""
			continue
		}
		target := refTargets[u.Name]
		if err := store.Refs.Update(u.Name, expectedOld, &target); err != nil {
			var mismatch *gotstore.RefCASMismatchError
			if errors.As(err, &mismatch) {
				expected := ""
				if u.Old != nil {
					expected = *u.Old
				}
				writeJSONError(w, "ref_conflict", "stale old hash", fmt.Sprintf("expected %s, got %s", expected, mismatch.Actual), http.StatusConflict)
				return
			}
			http.Error(w, fmt.Sprintf("set ref %s: %v", u.Name, err), http.StatusInternalServerError)
			return
		}
		applied[u.Name] = string(target)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "updated": applied})
}

func (h *Handler) setProtocolHeaders(w http.ResponseWriter) {
	w.Header().Set(gotProtocolHeader, serverProtocolVersion)
	w.Header().Set(gotCapabilitiesHeader, serverCapabilities)
	w.Header().Set(gotLimitsHeader, serverLimitsValue)
}

// writeJSONError writes a structured JSON error response.
func writeJSONError(w http.ResponseWriter, code string, message string, detail string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]string{"error": message, "code": code}
	if detail != "" {
		resp["detail"] = detail
	}
	json.NewEncoder(w).Encode(resp)
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
	if status == http.StatusNotFound && h.repoExists(owner, repo) {
		status = http.StatusForbidden
		err = errors.New(http.StatusText(http.StatusForbidden))
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="gothub"`)
	}
	http.Error(w, err.Error(), status)
	return false
}

func (h *Handler) repoExists(owner, repo string) bool {
	if h.getStore == nil {
		return false
	}
	store, err := h.getStore(owner, repo)
	return err == nil && store != nil
}

func parseRefUpdates(w http.ResponseWriter, r *http.Request) ([]refUpdateItem, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRefUpdateBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	_, hasStructuredUpdates := raw["updates"]

	var req refUpdateRequest
	if hasStructuredUpdates {
		if err := json.Unmarshal(body, &req); err != nil {
			return nil, fmt.Errorf("invalid JSON")
		}
		if len(req.Updates) == 0 {
			return nil, fmt.Errorf("at least one ref update is required")
		}
		out := make([]refUpdateItem, 0, len(req.Updates))
		seen := make(map[string]struct{}, len(req.Updates))
		for _, u := range req.Updates {
			name := strings.TrimSpace(u.Name)
			if name == "" {
				return nil, fmt.Errorf("ref update name is required")
			}
			if u.New == nil {
				return nil, fmt.Errorf("ref %s: new hash must be provided", name)
			}
			if _, exists := seen[name]; exists {
				return nil, fmt.Errorf("duplicate ref update for %s", name)
			}
			seen[name] = struct{}{}
			newHash := strings.TrimSpace(*u.New)
			var oldHash *string
			if u.Old != nil {
				v := strings.TrimSpace(*u.Old)
				oldHash = &v
			}
			out = append(out, refUpdateItem{
				Name: name,
				Old:  oldHash,
				New:  &newHash,
			})
		}
		return out, nil
	}

	var legacy map[string]string
	if err := json.Unmarshal(body, &legacy); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	if len(legacy) == 0 {
		return nil, fmt.Errorf("at least one ref update is required")
	}
	out := make([]refUpdateItem, 0, len(legacy))
	for name, hash := range legacy {
		n := strings.TrimSpace(name)
		if n == "" {
			return nil, fmt.Errorf("ref update name is required")
		}
		h := strings.TrimSpace(hash)
		out = append(out, refUpdateItem{Name: n, New: &h})
	}
	return out, nil
}

func objectTypeToPackType(t object.ObjectType) object.PackObjectType {
	switch t {
	case object.TypeCommit:
		return object.PackCommit
	case object.TypeTree:
		return object.PackTree
	case object.TypeBlob, object.TypeEntity, object.TypeEntityList:
		return object.PackBlob
	case object.TypeTag:
		return object.PackTag
	default:
		return object.PackBlob
	}
}

func packTypeToObjectType(t object.PackObjectType) object.ObjectType {
	switch t {
	case object.PackCommit:
		return object.TypeCommit
	case object.PackTree:
		return object.TypeTree
	case object.PackBlob:
		return object.TypeBlob
	case object.PackTag:
		return object.TypeTag
	default:
		return object.TypeBlob
	}
}

func parsePushedObjectType(raw string) (object.ObjectType, error) {
	switch object.ObjectType(strings.TrimSpace(raw)) {
	case object.TypeBlob, object.TypeTag, object.TypeTree, object.TypeCommit, object.TypeEntity, object.TypeEntityList:
		return object.ObjectType(strings.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("unsupported object type %q", raw)
	}
}

func isMissingRefErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not exist") || strings.Contains(s, "no such file")
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
	case object.TypeTag:
		tag, err := object.UnmarshalTag(data)
		if err != nil {
			return err
		}
		if err := requireRef(tag.TargetHash, object.TypeCommit, object.TypeTree, object.TypeBlob, object.TypeTag); err != nil {
			return fmt.Errorf("tag target: %w", err)
		}
		return nil
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
			entityListHash, ok, err := entityutil.ExtractAndWriteEntityList(store.Objects, fullPath, e.BlobHash)
			if err != nil {
				return "", false, err
			}
			if ok {
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
		case object.TypeTag:
			tag, err := store.ReadTag(h)
			if err != nil {
				return err
			}
			if err := walk(tag.TargetHash); err != nil {
				return err
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
