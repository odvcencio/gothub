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
	copied, err := s.copyParentVersionsIfTreeUnchanged(ctx, repoID, store, commitHash, commit)
	if err != nil {
		return err
	}
	if copied {
		return nil
	}

	snapshots, err := collectEntitySnapshots(store.Objects, commit.TreeHash, "")
	if err != nil {
		return err
	}

	parentVersions := make([]models.EntityVersion, 0)
	for _, p := range commit.Parents {
		versions, err := s.db.ListEntityVersionsByCommit(ctx, repoID, string(p))
		if err != nil {
			return err
		}
		parentVersions = append(parentVersions, versions...)
	}

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

	for i := range snapshots {
		snap := snapshots[i]
		stableID := pickStableID(repoID, string(commitHash), snap, byBody, bySig)

		if err := s.db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
			RepoID:          repoID,
			StableID:        stableID,
			Name:            snap.Name,
			DeclKind:        snap.DeclKind,
			Receiver:        snap.Receiver,
			FirstSeenCommit: string(commitHash),
			LastSeenCommit:  string(commitHash),
		}); err != nil {
			return err
		}

		if err := s.db.SetEntityVersion(ctx, &models.EntityVersion{
			RepoID:     repoID,
			StableID:   stableID,
			CommitHash: string(commitHash),
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

func (s *EntityLineageService) copyParentVersionsIfTreeUnchanged(ctx context.Context, repoID int64, store *gotstore.RepoStore, commitHash object.Hash, commit *object.CommitObj) (bool, error) {
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
	if parentCommit.TreeHash != commit.TreeHash {
		return false, nil
	}

	parentVersions, err := s.db.ListEntityVersionsByCommit(ctx, repoID, string(parentHash))
	if err != nil {
		return false, err
	}
	if len(parentVersions) == 0 {
		return false, nil
	}

	currentCommit := string(commitHash)
	for i := range parentVersions {
		parentVersion := parentVersions[i]
		if strings.TrimSpace(parentVersion.StableID) == "" {
			continue
		}

		if err := s.db.SetEntityVersion(ctx, &models.EntityVersion{
			RepoID:     repoID,
			StableID:   parentVersion.StableID,
			CommitHash: currentCommit,
			Path:       parentVersion.Path,
			EntityHash: parentVersion.EntityHash,
			BodyHash:   parentVersion.BodyHash,
			Name:       parentVersion.Name,
			DeclKind:   parentVersion.DeclKind,
			Receiver:   parentVersion.Receiver,
		}); err != nil {
			return false, err
		}

		if err := s.db.UpsertEntityIdentity(ctx, &models.EntityIdentity{
			RepoID:          repoID,
			StableID:        parentVersion.StableID,
			Name:            parentVersion.Name,
			DeclKind:        parentVersion.DeclKind,
			Receiver:        parentVersion.Receiver,
			FirstSeenCommit: parentVersion.CommitHash,
			LastSeenCommit:  currentCommit,
		}); err != nil {
			return false, err
		}
	}

	return true, nil
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
		if e.EntityListHash != "" {
			el, err := store.ReadEntityList(e.EntityListHash)
			if err != nil {
				continue
			}
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
			continue
		}

		blob, err := store.ReadBlob(e.BlobHash)
		if err != nil {
			continue
		}
		extracted, err := entity.Extract(fullPath, blob.Data)
		if err != nil {
			continue
		}
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
	}
	return snapshots, nil
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
