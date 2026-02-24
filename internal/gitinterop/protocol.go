package gitinterop

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

// SmartHTTPHandler implements the git smart HTTP protocol.
type SmartHTTPHandler struct {
	getStore     func(owner, repo string) (*gotstore.RepoStore, error)
	db           database.DB
	getRepo      func(ctx context.Context, owner, repo string) (int64, error) // returns repo ID
	authorize    func(r *http.Request, owner, repo string, write bool) (int, error)
	indexLineage func(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash) error
}

type refUpdate struct {
	oldHash, newHash GitHash
	refName          string
	storageRef       string
}

const (
	maxReceivePackBytes int64 = 256 << 20
	maxUploadPackBytes  int64 = 8 << 20
	gitZeroHash40             = "0000000000000000000000000000000000000000"
)

func NewSmartHTTPHandler(
	getStore func(owner, repo string) (*gotstore.RepoStore, error),
	db database.DB,
	getRepo func(ctx context.Context, owner, repo string) (int64, error),
	authorize func(r *http.Request, owner, repo string, write bool) (int, error),
	indexLineage func(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash) error,
) *SmartHTTPHandler {
	return &SmartHTTPHandler{
		getStore:     getStore,
		db:           db,
		getRepo:      getRepo,
		authorize:    authorize,
		indexLineage: indexLineage,
	}
}

// RegisterRoutes sets up git smart HTTP protocol routes.
func (h *SmartHTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /git/{owner}/{repo}/info/refs", h.handleInfoRefs)
	mux.HandleFunc("POST /git/{owner}/{repo}/git-upload-pack", h.handleUploadPack)
	mux.HandleFunc("POST /git/{owner}/{repo}/git-receive-pack", h.handleReceivePack)
}

// GET /git/{owner}/{repo}/info/refs?service=git-upload-pack|git-receive-pack
func (h *SmartHTTPHandler) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	svc := r.URL.Query().Get("service")

	if svc != "git-upload-pack" && svc != "git-receive-pack" {
		http.Error(w, "unsupported service", http.StatusBadRequest)
		return
	}
	if ok := h.authorizeRequest(w, r, owner, repo, svc == "git-receive-pack"); !ok {
		return
	}

	store, err := h.getStore(owner, repo)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	refs, err := store.Refs.ListAll()
	if err != nil {
		// Empty repo — return empty ref list
		refs = map[string]object.Hash{}
	}

	// Convert Got hashes to git hashes
	repoID, err := h.getRepo(r.Context(), owner, repo)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", svc))
	w.Header().Set("Cache-Control", "no-cache")

	// Service announcement
	w.Write(pktLine(fmt.Sprintf("# service=%s\n", svc)))
	w.Write(pktFlush())

	// Send refs
	first := true
	for name, gotHash := range refs {
		gitHash, err := h.db.GetGitHash(r.Context(), repoID, string(gotHash))
		if err != nil {
			// If no mapping exists, the hash might not have been pushed via git
			continue
		}
		refName := advertiseGitRefName(name)
		if first {
			// First ref includes capabilities
			w.Write(pktLine(fmt.Sprintf("%s %s\x00report-status delete-refs ofs-delta\n", gitHash, refName)))
			first = false
		} else {
			w.Write(pktLine(fmt.Sprintf("%s %s\n", gitHash, refName)))
		}
	}

	if first {
		// No refs — send zero-id with capabilities
		w.Write(pktLine(fmt.Sprintf("%s capabilities^{}\x00report-status delete-refs ofs-delta\n", strings.Repeat("0", 40))))
	}

	w.Write(pktFlush())
}

// POST /git/{owner}/{repo}/git-receive-pack
func (h *SmartHTTPHandler) handleReceivePack(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	if ok := h.authorizeRequest(w, r, owner, repo, true); !ok {
		return
	}

	store, err := h.getStore(owner, repo)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	repoID, err := h.getRepo(r.Context(), owner, repo)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	br := bufio.NewReader(http.MaxBytesReader(w, r.Body, maxReceivePackBytes))

	// Read ref update commands
	var updates []refUpdate

	for {
		line, err := readPktLine(br)
		if err != nil {
			if isRequestTooLarge(err) {
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "protocol error", http.StatusBadRequest)
			return
		}
		if line == nil {
			break // flush
		}
		s := strings.TrimRight(string(line), "\n")
		// Remove capabilities from first line
		if idx := strings.IndexByte(s, 0); idx >= 0 {
			s = s[:idx]
		}
		parts := strings.SplitN(s, " ", 3)
		if len(parts) != 3 {
			continue
		}
		updates = append(updates, refUpdate{
			oldHash:    GitHash(parts[0]),
			newHash:    GitHash(parts[1]),
			refName:    parts[2],
			storageRef: normalizeGitRefName(parts[2]),
		})
	}

	// Read packfile (rest of body)
	packData, err := io.ReadAll(br)
	if err != nil {
		if isRequestTooLarge(err) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "read packfile error", http.StatusBadRequest)
		return
	}

	if len(packData) > 0 {
		// Parse packfile and convert objects to Got format
		objects, err := ParsePackfile(bytes.NewReader(packData))
		if err != nil {
			h.sendReceivePackResult(w, fmt.Sprintf("unpack error: %v", err), nil, nil)
			return
		}

		knownGotByGit := make(map[string]string, len(objects))
		pendingMappings := make([]models.HashMapping, 0, len(objects))
		type treeModeRecord struct {
			gotTreeHash string
			modes       map[string]string
		}
		pendingTreeModes := make([]treeModeRecord, 0, len(objects))
		resolveGotHash := func(gitHash, mode string) (string, error) {
			if gotHash, ok := knownGotByGit[gitHash]; ok {
				return gotHash, nil
			}
			gotHash, err := h.db.GetGotHash(r.Context(), repoID, gitHash)
			if err == nil {
				knownGotByGit[gitHash] = gotHash
				return gotHash, nil
			}
			// Submodules (mode 160000) point at external git commits that may not
			// exist in this repo's object graph. Persist a synthetic blob mapping so
			// the tree entry can round-trip through Got and back to git unchanged.
			if mode == "160000" && errors.Is(err, sql.ErrNoRows) {
				submoduleBlobHash, writeErr := store.Objects.WriteBlob(&object.Blob{Data: []byte("submodule " + gitHash + "\n")})
				if writeErr != nil {
					return "", writeErr
				}
				gotHash = string(submoduleBlobHash)
				knownGotByGit[gitHash] = gotHash
				pendingMappings = append(pendingMappings, models.HashMapping{
					RepoID: repoID, GotHash: gotHash, GitHash: gitHash, ObjectType: "blob",
				})
				return gotHash, nil
			}
			return "", err
		}

		// Convert git objects to Got format.
		// Blobs can be written immediately; trees/commits may depend on hash mappings.
		var deferred []PackfileObject
		for _, obj := range objects {
			gitHash := GitHash(gitHashRaw(obj.Type, obj.Data))

			switch obj.Type {
			case OBJ_BLOB:
				gotHash, err := store.Objects.WriteBlob(&object.Blob{Data: obj.Data})
				if err != nil {
					h.sendReceivePackResult(w, fmt.Sprintf("unpack error: write blob: %v", err), nil, nil)
					return
				}
				knownGotByGit[string(gitHash)] = string(gotHash)
				pendingMappings = append(pendingMappings, models.HashMapping{
					RepoID: repoID, GotHash: string(gotHash), GitHash: string(gitHash), ObjectType: "blob",
				})
			case OBJ_TREE, OBJ_COMMIT:
				deferred = append(deferred, obj)
			}
		}

		for len(deferred) > 0 {
			progress := false
			nextDeferred := make([]PackfileObject, 0, len(deferred))

			for _, obj := range deferred {
				gitHash := GitHash(gitHashRaw(obj.Type, obj.Data))

				switch obj.Type {
				case OBJ_TREE:
					gotTree, treeModes, err := parseGitTree(obj.Data, resolveGotHash)
					if err != nil {
						if errors.Is(err, sql.ErrNoRows) {
							nextDeferred = append(nextDeferred, obj)
							continue
						}
						h.sendReceivePackResult(w, fmt.Sprintf("unpack error: parse tree: %v", err), nil, nil)
						return
					}
					gotHash, err := store.Objects.WriteTree(gotTree)
					if err != nil {
						h.sendReceivePackResult(w, fmt.Sprintf("unpack error: write tree: %v", err), nil, nil)
						return
					}
					knownGotByGit[string(gitHash)] = string(gotHash)
					pendingMappings = append(pendingMappings, models.HashMapping{
						RepoID: repoID, GotHash: string(gotHash), GitHash: string(gitHash), ObjectType: "tree",
					})
					pendingTreeModes = append(pendingTreeModes, treeModeRecord{
						gotTreeHash: string(gotHash),
						modes:       treeModes,
					})
					progress = true

				case OBJ_COMMIT:
					gotCommit, err := parseGitCommit(obj.Data, func(gitHash string) (string, error) {
						return resolveGotHash(gitHash, "")
					})
					if err != nil {
						if errors.Is(err, sql.ErrNoRows) {
							nextDeferred = append(nextDeferred, obj)
							continue
						}
						h.sendReceivePackResult(w, fmt.Sprintf("unpack error: parse commit: %v", err), nil, nil)
						return
					}
					gotHash, err := store.Objects.WriteCommit(gotCommit)
					if err != nil {
						h.sendReceivePackResult(w, fmt.Sprintf("unpack error: write commit: %v", err), nil, nil)
						return
					}
					knownGotByGit[string(gitHash)] = string(gotHash)
					pendingMappings = append(pendingMappings, models.HashMapping{
						RepoID: repoID, GotHash: string(gotHash), GitHash: string(gitHash), ObjectType: "commit",
					})
					progress = true
				}
			}

			if !progress {
				h.sendReceivePackResult(w, "unpack error: unresolved object dependencies", nil, nil)
				return
			}
			deferred = nextDeferred
		}

		if err := h.db.SetHashMappings(r.Context(), pendingMappings); err != nil {
			h.sendReceivePackResult(w, fmt.Sprintf("unpack error: persist hash mappings: %v", err), nil, nil)
			return
		}
		for _, tm := range pendingTreeModes {
			if err := h.db.SetGitTreeEntryModes(r.Context(), repoID, tm.gotTreeHash, tm.modes); err != nil {
				h.sendReceivePackResult(w, fmt.Sprintf("unpack error: persist tree modes: %v", err), nil, nil)
				return
			}
		}

		// Run entity extraction and rewrite trees/commits so entity lists are reachable.
		entityCommitMappings, err := h.extractEntitiesForCommits(r.Context(), store, repoID, updates)
		if err != nil {
			h.sendReceivePackResult(w, fmt.Sprintf("unpack error: entity extraction: %v", err), nil, nil)
			return
		}
		if len(entityCommitMappings) > 0 {
			if err := h.db.SetHashMappings(r.Context(), entityCommitMappings); err != nil {
				h.sendReceivePackResult(w, fmt.Sprintf("unpack error: persist entity commit mappings: %v", err), nil, nil)
				return
			}
		}
	}

	// Update refs
	refErrors := make(map[string]string, len(updates))
	for _, u := range updates {
		currentGitHash, err := h.currentGitRefHash(r.Context(), store, repoID, u.storageRef)
		if err != nil {
			refErrors[u.refName] = err.Error()
			continue
		}
		if currentGitHash != string(u.oldHash) {
			refErrors[u.refName] = fmt.Sprintf("stale old hash (expected %s, got %s)", string(u.oldHash), currentGitHash)
			continue
		}

		if string(u.newHash) == gitZeroHash40 {
			if err := store.Refs.Delete(u.storageRef); err != nil {
				refErrors[u.refName] = err.Error()
			}
			continue
		}
		gotHash, err := h.db.GetGotHash(r.Context(), repoID, string(u.newHash))
		if err != nil {
			refErrors[u.refName] = "missing object mapping"
			continue
		}
		if h.indexLineage != nil {
			if err := h.indexLineage(r.Context(), repoID, store, object.Hash(gotHash)); err != nil {
				refErrors[u.refName] = "lineage index failed"
				continue
			}
		}
		if err := store.Refs.Set(u.storageRef, object.Hash(gotHash)); err != nil {
			refErrors[u.refName] = err.Error()
		}
	}

	h.sendReceivePackResult(w, "", updates, refErrors)
}

func (h *SmartHTTPHandler) currentGitRefHash(ctx context.Context, store *gotstore.RepoStore, repoID int64, storageRef string) (string, error) {
	gotHash, err := store.Refs.Get(storageRef)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(strings.ToLower(err.Error()), "no such file") {
			return gitZeroHash40, nil
		}
		return "", fmt.Errorf("read current ref: %w", err)
	}
	gitHash, err := h.db.GetGitHash(ctx, repoID, string(gotHash))
	if err != nil {
		return "", fmt.Errorf("resolve current ref mapping: %w", err)
	}
	return gitHash, nil
}

// POST /git/{owner}/{repo}/git-upload-pack
func (h *SmartHTTPHandler) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	if ok := h.authorizeRequest(w, r, owner, repo, false); !ok {
		return
	}

	store, err := h.getStore(owner, repo)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	repoID, err := h.getRepo(r.Context(), owner, repo)
	if err != nil {
		http.Error(w, "repository not found", http.StatusNotFound)
		return
	}

	br := bufio.NewReader(http.MaxBytesReader(w, r.Body, maxUploadPackBytes))

	// Read want/have negotiation
	var wants []GitHash
	var haves []GitHash

	for {
		line, err := readPktLine(br)
		if err != nil {
			if isRequestTooLarge(err) {
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "protocol error", http.StatusBadRequest)
			return
		}
		if line == nil {
			break
		}
		s := strings.TrimRight(string(line), "\n")
		// Strip capabilities from first want line
		if idx := strings.IndexByte(s, 0); idx >= 0 {
			s = s[:idx]
		}
		if strings.HasPrefix(s, "want ") {
			wants = append(wants, GitHash(strings.Fields(s)[1]))
		} else if strings.HasPrefix(s, "have ") {
			haves = append(haves, GitHash(strings.Fields(s)[1]))
		} else if s == "done" {
			break
		}
	}

	// Read remaining "done" if not already consumed
	for {
		line, err := readPktLine(br)
		if err != nil {
			if isRequestTooLarge(err) {
				http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
				return
			}
			break
		}
		if line == nil {
			break
		}
		if strings.TrimSpace(string(line)) == "done" {
			break
		}
	}

	// Collect objects to send
	haveSet := make(map[object.Hash]bool)
	for _, gh := range haves {
		if gotHash, err := h.db.GetGotHash(r.Context(), repoID, string(gh)); err == nil {
			haveSet[object.Hash(gotHash)] = true
		}
	}

	var packObjects []PackfileObject

	for _, wantGitHash := range wants {
		gotHash, err := h.db.GetGotHash(r.Context(), repoID, string(wantGitHash))
		if err != nil {
			continue
		}

		// Walk object graph from this commit, collecting objects the client doesn't have
		missing, err := walkGotObjects(store.Objects, object.Hash(gotHash), func(h object.Hash) bool {
			return haveSet[h]
		})
		if err != nil {
			continue
		}

		for _, m := range missing {
			objType, data, err := store.Objects.Read(m)
			if err != nil {
				continue
			}
			gitType := gotTypeToPackType(objType)
			gitData, err := convertGotToGitData(m, objType, data, store.Objects, r.Context(), h.db, repoID)
			if err != nil {
				continue
			}
			packObjects = append(packObjects, PackfileObject{Type: gitType, Data: gitData})
		}
	}

	// Build and send packfile
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")

	w.Write(pktLine("NAK\n"))

	if len(packObjects) > 0 {
		packData, err := BuildPackfile(packObjects)
		if err != nil {
			return
		}
		w.Write(packData)
	}

	w.Write(pktFlush())
}

func (h *SmartHTTPHandler) sendReceivePackResult(w http.ResponseWriter, errMsg string, updates []refUpdate, refErrors map[string]string) {
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	if errMsg != "" {
		w.Write(pktLine(fmt.Sprintf("unpack %s\n", errMsg)))
	} else {
		w.Write(pktLine("unpack ok\n"))
		for _, u := range updates {
			if msg, failed := refErrors[u.refName]; failed && msg != "" {
				w.Write(pktLine(fmt.Sprintf("ng %s %s\n", u.refName, msg)))
				continue
			}
			w.Write(pktLine(fmt.Sprintf("ok %s\n", u.refName)))
		}
	}
	w.Write(pktFlush())
}

// extractEntitiesForCommits rewrites pushed commits with trees that reference entity lists.
// Returns commit hash-mapping overrides for the original git commit hashes.
func (h *SmartHTTPHandler) extractEntitiesForCommits(ctx context.Context, store *gotstore.RepoStore, repoID int64, updates []refUpdate) ([]models.HashMapping, error) {
	overrides := make([]models.HashMapping, 0, len(updates))
	for _, u := range updates {
		if string(u.newHash) == strings.Repeat("0", 40) {
			continue
		}
		gotHash, err := h.db.GetGotHash(ctx, repoID, string(u.newHash))
		if err != nil {
			continue
		}
		newCommitHash, changed, err := h.rewriteCommitWithEntities(ctx, store, repoID, object.Hash(gotHash))
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		overrides = append(overrides, models.HashMapping{
			RepoID:     repoID,
			GotHash:    string(newCommitHash),
			GitHash:    string(u.newHash),
			ObjectType: "commit",
		})
	}
	return overrides, nil
}

func (h *SmartHTTPHandler) rewriteCommitWithEntities(ctx context.Context, store *gotstore.RepoStore, repoID int64, commitHash object.Hash) (object.Hash, bool, error) {
	commit, err := store.Objects.ReadCommit(commitHash)
	if err != nil {
		return "", false, err
	}
	newTreeHash, changed, err := h.rewriteTreeWithEntities(ctx, store, repoID, commit.TreeHash, "")
	if err != nil {
		return "", false, err
	}
	if !changed {
		return commitHash, false, nil
	}
	updatedCommit := *commit
	updatedCommit.TreeHash = newTreeHash
	newCommitHash, err := store.Objects.WriteCommit(&updatedCommit)
	if err != nil {
		return "", false, err
	}
	return newCommitHash, true, nil
}

func (h *SmartHTTPHandler) rewriteTreeWithEntities(ctx context.Context, store *gotstore.RepoStore, repoID int64, treeHash object.Hash, prefix string) (object.Hash, bool, error) {
	tree, err := store.Objects.ReadTree(treeHash)
	if err != nil {
		return "", false, err
	}
	modeMap, _ := h.db.GetGitTreeEntryModes(ctx, repoID, string(treeHash))

	changed := false
	updatedEntries := make([]object.TreeEntry, len(tree.Entries))
	for i, e := range tree.Entries {
		entry := e
		fullPath := e.Name
		if prefix != "" {
			fullPath = prefix + "/" + e.Name
		}
		if e.IsDir {
			newSubtreeHash, subtreeChanged, err := h.rewriteTreeWithEntities(ctx, store, repoID, e.SubtreeHash, fullPath)
			if err != nil {
				return "", false, err
			}
			if subtreeChanged {
				entry.SubtreeHash = newSubtreeHash
				changed = true
			}
		} else if e.EntityListHash == "" {
			mode := modeMap[e.Name]
			if mode != "" && mode != "100644" && mode != "100755" {
				updatedEntries[i] = entry
				continue
			}
			blob, err := store.Objects.ReadBlob(e.BlobHash)
			if err != nil {
				return "", false, err
			}
			el, err := entity.Extract(fullPath, blob.Data)
			if err == nil && len(el.Entities) > 0 {
				entityRefs := make([]object.Hash, 0, len(el.Entities))
				for _, ent := range el.Entities {
					entObj := &object.EntityObj{
						Kind:     entityKindToString(ent.Kind),
						Name:     ent.Name,
						DeclKind: ent.DeclKind,
						Receiver: ent.Receiver,
						Body:     ent.Body,
						BodyHash: object.Hash(ent.BodyHash),
					}
					entHash, err := store.Objects.WriteEntity(entObj)
					if err != nil {
						return "", false, err
					}
					entityRefs = append(entityRefs, entHash)
				}
				elObj := &object.EntityListObj{
					Language:   el.Language,
					Path:       fullPath,
					EntityRefs: entityRefs,
				}
				entityListHash, err := store.Objects.WriteEntityList(elObj)
				if err != nil {
					return "", false, err
				}
				entry.EntityListHash = entityListHash
				changed = true
			}
		}
		updatedEntries[i] = entry
	}

	if !changed {
		return treeHash, false, nil
	}
	newTreeHash, err := store.Objects.WriteTree(&object.TreeObj{Entries: updatedEntries})
	if err != nil {
		return "", false, err
	}
	if modes, err := h.db.GetGitTreeEntryModes(ctx, repoID, string(treeHash)); err == nil && len(modes) > 0 {
		_ = h.db.SetGitTreeEntryModes(ctx, repoID, string(newTreeHash), modes)
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

// --- conversion helpers ---

// parseGitTree converts git tree binary data to a Got TreeObj and returns git modes by entry name.
func parseGitTree(data []byte, resolveGotHash func(gitHash, mode string) (string, error)) (*object.TreeObj, map[string]string, error) {
	var entries []object.TreeEntry
	modes := make(map[string]string)
	buf := bytes.NewReader(data)
	for buf.Len() > 0 {
		// Read mode
		var mode []byte
		for {
			b, err := buf.ReadByte()
			if err != nil {
				return nil, nil, err
			}
			if b == ' ' {
				break
			}
			mode = append(mode, b)
		}
		// Read name (null-terminated)
		var name []byte
		for {
			b, err := buf.ReadByte()
			if err != nil {
				return nil, nil, err
			}
			if b == 0 {
				break
			}
			name = append(name, b)
		}
		// Read 20-byte hash
		var hashBytes [20]byte
		if _, err := io.ReadFull(buf, hashBytes[:]); err != nil {
			return nil, nil, err
		}
		gitHash := bytesToHex(hashBytes[:])

		modeStr := string(mode)
		isDir := modeStr == "40000"
		gotHash, err := resolveGotHash(gitHash, modeStr)
		if err != nil {
			return nil, nil, fmt.Errorf("missing hash mapping for tree entry %s: %w", gitHash, err)
		}

		entry := object.TreeEntry{
			Name:  string(name),
			IsDir: isDir,
		}
		if isDir {
			entry.SubtreeHash = object.Hash(gotHash)
		} else {
			entry.BlobHash = object.Hash(gotHash)
		}
		entries = append(entries, entry)
		modes[entry.Name] = modeStr
	}
	return &object.TreeObj{Entries: entries}, modes, nil
}

// parseGitCommit converts git commit text to a Got CommitObj.
func parseGitCommit(data []byte, resolveGotHash func(gitHash string) (string, error)) (*object.CommitObj, error) {
	lines := strings.Split(string(data), "\n")
	c := &object.CommitObj{}
	inBody := false
	var bodyLines []string

	for _, line := range lines {
		if inBody {
			bodyLines = append(bodyLines, line)
			continue
		}
		if line == "" {
			inBody = true
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "tree":
			gotHash, err := resolveGotHash(parts[1])
			if err != nil {
				return nil, fmt.Errorf("missing tree hash mapping %s: %w", parts[1], err)
			}
			c.TreeHash = object.Hash(gotHash)
		case "parent":
			gotHash, err := resolveGotHash(parts[1])
			if err != nil {
				return nil, fmt.Errorf("missing parent hash mapping %s: %w", parts[1], err)
			}
			c.Parents = append(c.Parents, object.Hash(gotHash))
		case "author":
			// "Name <email> timestamp timezone"
			c.Author = extractAuthorName(parts[1])
			c.Timestamp = extractTimestamp(parts[1])
		}
	}
	if c.TreeHash == "" {
		return nil, fmt.Errorf("commit is missing tree hash")
	}
	c.Message = strings.Join(bodyLines, "\n")
	return c, nil
}

func extractAuthorName(s string) string {
	if idx := strings.LastIndex(s, ">"); idx >= 0 {
		return strings.TrimSpace(s[:idx+1])
	}
	return s
}

func extractTimestamp(s string) int64 {
	// Format: "Name <email> 1234567890 +0000"
	parts := strings.Fields(s)
	if len(parts) >= 2 {
		for i := len(parts) - 1; i >= 0; i-- {
			if len(parts[i]) > 5 && parts[i][0] != '+' && parts[i][0] != '-' {
				var ts int64
				fmt.Sscanf(parts[i], "%d", &ts)
				if ts > 0 {
					return ts
				}
			}
		}
	}
	return 0
}

// walkGotObjects walks the Got object graph collecting objects the client doesn't have.
func walkGotObjects(store *object.Store, root object.Hash, has func(object.Hash) bool) ([]object.Hash, error) {
	var missing []object.Hash
	seen := make(map[object.Hash]bool)

	var walk func(h object.Hash) error
	walk = func(h object.Hash) error {
		if h == "" || seen[h] || has(h) {
			return nil
		}
		seen[h] = true
		if !store.Has(h) {
			return fmt.Errorf("object %s missing", h)
		}
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

// convertGotToGitData converts a Got object's data to git format.
func convertGotToGitData(gotHash object.Hash, objType object.ObjectType, data []byte, store *object.Store, ctx context.Context, db database.DB, repoID int64) ([]byte, error) {
	switch objType {
	case object.TypeBlob:
		return data, nil // blob data is the same
	case object.TypeCommit:
		commit, err := object.UnmarshalCommit(data)
		if err != nil {
			return nil, err
		}
		treeGitHash := getGitHash(ctx, db, repoID, string(commit.TreeHash))
		var parentGitHashes []GitHash
		for _, p := range commit.Parents {
			parentGitHashes = append(parentGitHashes, GitHash(getGitHash(ctx, db, repoID, string(p))))
		}
		_, gitData := GotToGitCommit(commit, GitHash(treeGitHash), parentGitHashes)
		return gitData, nil
	case object.TypeTree:
		tree, err := object.UnmarshalTree(data)
		if err != nil {
			return nil, err
		}
		entryHashes := make(map[string]GitHash)
		for _, e := range tree.Entries {
			var h object.Hash
			if e.IsDir {
				h = e.SubtreeHash
			} else {
				h = e.BlobHash
			}
			entryHashes[e.Name] = GitHash(getGitHash(ctx, db, repoID, string(h)))
		}
		modeMap, _ := db.GetGitTreeEntryModes(ctx, repoID, string(gotHash))
		_, gitData := GotToGitTree(tree, entryHashes, modeMap)
		return gitData, nil
	default:
		return data, nil
	}
}

func getGitHash(ctx context.Context, db database.DB, repoID int64, gotHash string) string {
	h, err := db.GetGitHash(ctx, repoID, gotHash)
	if err != nil {
		return strings.Repeat("0", 40) // fallback
	}
	return h
}

func gotTypeToPackType(t object.ObjectType) int {
	switch t {
	case object.TypeCommit:
		return OBJ_COMMIT
	case object.TypeTree:
		return OBJ_TREE
	case object.TypeBlob:
		return OBJ_BLOB
	default:
		return OBJ_BLOB
	}
}

func (h *SmartHTTPHandler) authorizeRequest(w http.ResponseWriter, r *http.Request, owner, repo string, write bool) bool {
	if h.authorize == nil {
		return true
	}
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

func normalizeGitRefName(refName string) string {
	refName = strings.TrimSpace(refName)
	return strings.TrimPrefix(refName, "refs/")
}

func advertiseGitRefName(storageRef string) string {
	storageRef = strings.TrimSpace(storageRef)
	if storageRef == "" {
		return storageRef
	}
	if strings.HasPrefix(storageRef, "refs/") {
		return storageRef
	}
	return "refs/" + storageRef
}

func isRequestTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
