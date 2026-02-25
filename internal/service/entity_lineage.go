package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
	"github.com/odvcencio/gothub/internal/database"
	"github.com/odvcencio/gothub/internal/gotstore"
	"github.com/odvcencio/gothub/internal/models"
)

type EntityLineageService struct {
	db database.DB
}

func NewEntityLineageService(db database.DB) *EntityLineageService {
	return &EntityLineageService{db: db}
}

type entitySnapshot struct {
	Path       string
	EntityHash string
	BodyHash   string
	Kind       string
	Name       string
	DeclKind   string
	Receiver   string
}

// IndexCommit assigns stable entity identities for a commit and its ancestors.
func (s *EntityLineageService) IndexCommit(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash) error {
	seen := make(map[object.Hash]bool)
	var walk func(h object.Hash) error
	walk = func(h object.Hash) error {
		if h == "" || seen[h] {
			return nil
		}
		seen[h] = true

		done, err := s.db.HasEntityVersionsForCommit(ctx, repoID, string(h))
		if err == nil && done {
			return nil
		}

		commit, err := store.Objects.ReadCommit(h)
		if err != nil {
			return err
		}
		for _, p := range commit.Parents {
			if err := walk(p); err != nil {
				return err
			}
		}
		return s.indexCommitEntities(ctx, repoID, store, h, commit)
	}
	return walk(commitHash)
}

func (s *EntityLineageService) indexCommitEntities(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash, commit *object.CommitObj) error {
	done, err := s.db.HasEntityVersionsForCommit(ctx, repoID, string(commitHash))
	if err == nil && done {
		return nil
	}
	handled, err := s.indexCommitEntitiesSingleParentIncremental(ctx, repoID, store, commitHash, commit)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	snapshots, err := collectEntitySnapshots(store.Objects, commit.TreeHash, "")
	if err != nil {
		return err
	}

	parentVersions, err := s.listParentEntityVersions(ctx, repoID, commit.Parents)
	if err != nil {
		return err
	}
	return s.persistEntitySnapshots(ctx, repoID, string(commitHash), snapshots, parentVersions)
}

func (s *EntityLineageService) indexCommitEntitiesSingleParentIncremental(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash, commit *object.CommitObj) (bool, error) {
	if commit == nil || len(commit.Parents) != 1 {
		return false, nil
	}
	parentHash := commit.Parents[0]
	if parentHash == "" {
		return false, nil
	}

	parentCommit, err := store.Objects.ReadCommit(parentHash)
	if err != nil {
		return false, err
	}

	parentVersions, err := s.db.ListEntityVersionsByCommit(ctx, repoID, string(parentHash))
	if err != nil {
		return false, err
	}
	if len(parentVersions) == 0 {
		return false, nil
	}

	currentCommit := string(commitHash)
	if parentCommit.TreeHash == commit.TreeHash {
		if err := s.copyParentVersions(ctx, repoID, currentCommit, parentVersions, nil); err != nil {
			return false, err
		}
		return true, nil
	}

	changedPaths, commitFilesByPath, err := changedFilesBetweenTrees(store.Objects, parentCommit.TreeHash, commit.TreeHash)
	if err != nil {
		return false, err
	}

	if err := s.copyParentVersions(ctx, repoID, currentCommit, parentVersions, changedPaths); err != nil {
		return false, err
	}
	if len(changedPaths) == 0 {
		return true, nil
	}

	changedSnapshots := collectEntitySnapshotsForChangedPaths(store.Objects, commitFilesByPath, changedPaths)
	if err := s.persistEntitySnapshots(ctx, repoID, currentCommit, changedSnapshots, parentVersions); err != nil {
		return false, err
	}
	return true, nil
}

func (s *EntityLineageService) listParentEntityVersions(ctx context.Context, repoID int64, parents []object.Hash) ([]models.EntityVersion, error) {
	parentVersions := make([]models.EntityVersion, 0)
	for _, p := range parents {
		versions, err := s.db.ListEntityVersionsByCommit(ctx, repoID, string(p))
		if err != nil {
			return nil, err
		}
		parentVersions = append(parentVersions, versions...)
	}
	return parentVersions, nil
}

func (s *EntityLineageService) persistEntitySnapshots(ctx context.Context, repoID int64, commitHash string, snapshots []entitySnapshot, parentVersions []models.EntityVersion) error {
	byBody, bySig := buildStableIDLookup(parentVersions)
	for i := range snapshots {
		snap := snapshots[i]
		stableID := pickStableID(repoID, commitHash, snap, byBody, bySig)

		if err := s.db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
			RepoID:          repoID,
			StableID:        stableID,
			Name:            snap.Name,
			DeclKind:        snap.DeclKind,
			Receiver:        snap.Receiver,
			FirstSeenCommit: commitHash,
			LastSeenCommit:  commitHash,
		}); err != nil {
			return err
		}

		if err := s.db.SetEntityVersion(ctx, &models.EntityVersion{
			RepoID:     repoID,
			StableID:   stableID,
			CommitHash: commitHash,
			Path:       snap.Path,
			EntityHash: snap.EntityHash,
			BodyHash:   snap.BodyHash,
			Name:       snap.Name,
			DeclKind:   snap.DeclKind,
			Receiver:   snap.Receiver,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *EntityLineageService) copyParentVersions(ctx context.Context, repoID int64, commitHash string, parentVersions []models.EntityVersion, changedPaths map[string]struct{}) error {
	for i := range parentVersions {
		parentVersion := parentVersions[i]
		if strings.TrimSpace(parentVersion.StableID) == "" {
			continue
		}
		if changedPaths != nil {
			if _, changed := changedPaths[parentVersion.Path]; changed {
				continue
			}
		}

		if err := s.db.SetEntityVersion(ctx, &models.EntityVersion{
			RepoID:     repoID,
			StableID:   parentVersion.StableID,
			CommitHash: commitHash,
			Path:       parentVersion.Path,
			EntityHash: parentVersion.EntityHash,
			BodyHash:   parentVersion.BodyHash,
			Name:       parentVersion.Name,
			DeclKind:   parentVersion.DeclKind,
			Receiver:   parentVersion.Receiver,
		}); err != nil {
			return err
		}

		if err := s.db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
			RepoID:          repoID,
			StableID:        parentVersion.StableID,
			Name:            parentVersion.Name,
			DeclKind:        parentVersion.DeclKind,
			Receiver:        parentVersion.Receiver,
			FirstSeenCommit: parentVersion.CommitHash,
			LastSeenCommit:  commitHash,
		}); err != nil {
			return err
		}
	}
	return nil
}

func buildStableIDLookup(parentVersions []models.EntityVersion) (map[string][]string, map[string][]string) {
	byBody := make(map[string][]string)
	bySig := make(map[string][]string)
	for i := range parentVersions {
		v := parentVersions[i]
		if strings.TrimSpace(v.StableID) == "" {
			continue
		}
		if strings.TrimSpace(v.BodyHash) != "" {
			appendUniqueString(byBody, v.BodyHash, v.StableID)
		}
		appendUniqueString(bySig, signatureKey(v.Name, v.DeclKind, v.Receiver), v.StableID)
	}
	return byBody, bySig
}

func changedFilesBetweenTrees(store *object.Store, parentTreeHash, commitTreeHash object.Hash) (map[string]struct{}, map[string]FileEntry, error) {
	parentFiles, err := flattenTree(store, parentTreeHash, "")
	if err != nil {
		return nil, nil, err
	}
	commitFiles, err := flattenTree(store, commitTreeHash, "")
	if err != nil {
		return nil, nil, err
	}

	parentFilesByPath := fileEntriesByPath(parentFiles)
	commitFilesByPath := fileEntriesByPath(commitFiles)
	changedPaths := make(map[string]struct{})

	for path, commitFile := range commitFilesByPath {
		parentFile, ok := parentFilesByPath[path]
		if !ok || parentFile.BlobHash != commitFile.BlobHash || parentFile.EntityListHash != commitFile.EntityListHash {
			changedPaths[path] = struct{}{}
		}
	}
	for path := range parentFilesByPath {
		if _, ok := commitFilesByPath[path]; !ok {
			changedPaths[path] = struct{}{}
		}
	}

	return changedPaths, commitFilesByPath, nil
}

func fileEntriesByPath(files []FileEntry) map[string]FileEntry {
	byPath := make(map[string]FileEntry, len(files))
	for i := range files {
		byPath[files[i].Path] = files[i]
	}
	return byPath
}

func collectEntitySnapshotsForChangedPaths(store *object.Store, commitFilesByPath map[string]FileEntry, changedPaths map[string]struct{}) []entitySnapshot {
	paths := make([]string, 0, len(changedPaths))
	for path := range changedPaths {
		if _, exists := commitFilesByPath[path]; exists {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	snapshots := make([]entitySnapshot, 0)
	for i := range paths {
		path := paths[i]
		snapshots = append(snapshots, collectEntitySnapshotsForFile(store, path, commitFilesByPath[path])...)
	}
	return snapshots
}

func collectEntitySnapshots(store *object.Store, treeHash object.Hash, prefix string) ([]entitySnapshot, error) {
	tree, err := store.ReadTree(treeHash)
	if err != nil {
		return nil, err
	}
	snapshots := make([]entitySnapshot, 0)
	for _, e := range tree.Entries {
		fullPath := e.Name
		if prefix != "" {
			fullPath = prefix + "/" + e.Name
		}
		if e.IsDir {
			child, err := collectEntitySnapshots(store, e.SubtreeHash, fullPath)
			if err != nil {
				return nil, err
			}
			snapshots = append(snapshots, child...)
			continue
		}
		snapshots = append(snapshots, collectEntitySnapshotsForFile(store, fullPath, FileEntry{
			Path:           fullPath,
			BlobHash:       string(e.BlobHash),
			EntityListHash: string(e.EntityListHash),
		})...)
	}
	return snapshots, nil
}

func collectEntitySnapshotsForFile(store *object.Store, fullPath string, file FileEntry) []entitySnapshot {
	if file.EntityListHash != "" {
		el, err := store.ReadEntityList(object.Hash(file.EntityListHash))
		if err != nil {
			return nil
		}
		snapshots := make([]entitySnapshot, 0, len(el.EntityRefs))
		for _, ref := range el.EntityRefs {
			ent, err := store.ReadEntity(ref)
			if err != nil {
				continue
			}
			bodyHash := strings.TrimSpace(string(ent.BodyHash))
			if bodyHash == "" {
				bodyHash = string(object.HashBytes(ent.Body))
			}
			snapshots = append(snapshots, entitySnapshot{
				Path:       fullPath,
				EntityHash: string(ref),
				BodyHash:   bodyHash,
				Kind:       ent.Kind,
				Name:       ent.Name,
				DeclKind:   ent.DeclKind,
				Receiver:   ent.Receiver,
			})
		}
		return snapshots
	}

	blob, err := store.ReadBlob(object.Hash(file.BlobHash))
	if err != nil {
		return nil
	}
	extracted, err := entity.Extract(fullPath, blob.Data)
	if err != nil {
		return nil
	}

	snapshots := make([]entitySnapshot, 0, len(extracted.Entities))
	for _, ent := range extracted.Entities {
		bodyHash := strings.TrimSpace(ent.BodyHash)
		if bodyHash == "" {
			bodyHash = string(object.HashBytes(ent.Body))
		}
		syntheticHash := syntheticEntityHash(fullPath, ent.Name, ent.DeclKind, ent.Receiver, bodyHash)
		snapshots = append(snapshots, entitySnapshot{
			Path:       fullPath,
			EntityHash: syntheticHash,
			BodyHash:   bodyHash,
			Kind:       extractedEntityKindToString(ent.Kind),
			Name:       ent.Name,
			DeclKind:   ent.DeclKind,
			Receiver:   ent.Receiver,
		})
	}
	return snapshots
}

func extractedEntityKindToString(k entity.EntityKind) string {
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

func syntheticEntityHash(path, name, declKind, receiver, bodyHash string) string {
	seed := fmt.Sprintf("synthetic:%s:%s:%s:%s:%s", path, name, declKind, receiver, bodyHash)
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func appendUniqueString(target map[string][]string, key, value string) {
	values := target[key]
	for _, existing := range values {
		if existing == value {
			return
		}
	}
	target[key] = append(values, value)
}

func signatureKey(name, declKind, receiver string) string {
	return strings.TrimSpace(name) + "\x00" + strings.TrimSpace(declKind) + "\x00" + strings.TrimSpace(receiver)
}

func pickStableID(repoID int64, commitHash string, snap entitySnapshot, byBody, bySig map[string][]string) string {
	if ids := byBody[snap.BodyHash]; len(ids) > 0 {
		sort.Strings(ids)
		return ids[0]
	}
	if ids := bySig[signatureKey(snap.Name, snap.DeclKind, snap.Receiver)]; len(ids) > 0 {
		sort.Strings(ids)
		return ids[0]
	}
	seed := fmt.Sprintf("lineage:%d:%s:%s:%s:%s:%s:%s", repoID, commitHash, snap.Path, snap.Name, snap.DeclKind, snap.Receiver, snap.BodyHash)
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:16])
}
