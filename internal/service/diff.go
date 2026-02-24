package service

import (
	"context"
	"fmt"

	"github.com/odvcencio/got/pkg/diff"
	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
)

// EntityInfo represents a single entity for API responses.
type EntityInfo struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	DeclKind  string `json:"decl_kind"`
	Receiver  string `json:"receiver,omitempty"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	BodyHash  string `json:"body_hash"`
}

// EntityListResponse holds the entities for a file.
type EntityListResponse struct {
	Language string       `json:"language"`
	Path     string       `json:"path"`
	Entities []EntityInfo `json:"entities"`
}

// EntityChangeInfo represents a single entity-level change for API responses.
type EntityChangeInfo struct {
	Type   string `json:"type"` // "added", "removed", "modified"
	Key    string `json:"key"`
	Before *EntityInfo `json:"before,omitempty"`
	After  *EntityInfo `json:"after,omitempty"`
}

// FileDiffResponse holds the entity-level diff for a file.
type FileDiffResponse struct {
	Path    string             `json:"path"`
	Changes []EntityChangeInfo `json:"changes"`
}

// DiffResponse holds diffs across multiple files.
type DiffResponse struct {
	Base  string             `json:"base"`
	Head  string             `json:"head"`
	Files []FileDiffResponse `json:"files"`
}

type DiffService struct {
	repoSvc   *RepoService
	browseSvc *BrowseService
}

func NewDiffService(repoSvc *RepoService, browseSvc *BrowseService) *DiffService {
	return &DiffService{repoSvc: repoSvc, browseSvc: browseSvc}
}

// ExtractEntities extracts entities from a file at the given ref/path.
func (s *DiffService) ExtractEntities(ctx context.Context, owner, repo, ref, filePath string) (*EntityListResponse, error) {
	blob, err := s.browseSvc.GetBlob(ctx, owner, repo, ref, filePath)
	if err != nil {
		return nil, err
	}
	el, err := entity.Extract(filePath, blob.Data)
	if err != nil {
		return nil, fmt.Errorf("extract entities: %w", err)
	}
	return entityListToResponse(el), nil
}

// DiffRefs computes entity-level diffs between two refs (branches/commits).
func (s *DiffService) DiffRefs(ctx context.Context, owner, repo, baseRef, headRef string) (*DiffResponse, error) {
	store, err := s.repoSvc.OpenStore(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	baseHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, baseRef)
	if err != nil {
		return nil, fmt.Errorf("resolve base ref: %w", err)
	}
	headHash, err := s.browseSvc.ResolveRef(ctx, owner, repo, headRef)
	if err != nil {
		return nil, fmt.Errorf("resolve head ref: %w", err)
	}

	baseCommit, err := store.Objects.ReadCommit(baseHash)
	if err != nil {
		return nil, fmt.Errorf("read base commit: %w", err)
	}
	headCommit, err := store.Objects.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read head commit: %w", err)
	}

	// Flatten both trees to get all files
	baseFiles, err := flattenTree(store.Objects, baseCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten base tree: %w", err)
	}
	headFiles, err := flattenTree(store.Objects, headCommit.TreeHash, "")
	if err != nil {
		return nil, fmt.Errorf("flatten head tree: %w", err)
	}

	// Index files by path
	baseMap := make(map[string]FileEntry, len(baseFiles))
	for _, f := range baseFiles {
		baseMap[f.Path] = f
	}
	headMap := make(map[string]FileEntry, len(headFiles))
	for _, f := range headFiles {
		headMap[f.Path] = f
	}

	// Find changed files
	var fileDiffs []FileDiffResponse

	// Files in head (modified or added)
	for path, headEntry := range headMap {
		baseEntry, exists := baseMap[path]
		if exists && baseEntry.BlobHash == headEntry.BlobHash {
			continue // unchanged
		}
		var baseData, headData []byte
		if exists {
			baseData, err = readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				continue
			}
		}
		headData, err = readBlobData(store.Objects, object.Hash(headEntry.BlobHash))
		if err != nil {
			continue
		}
		fd, err := diff.DiffFiles(path, baseData, headData)
		if err != nil {
			continue
		}
		if len(fd.Changes) > 0 {
			fileDiffs = append(fileDiffs, fileDiffToResponse(fd))
		}
	}

	// Files only in base (deleted)
	for path, baseEntry := range baseMap {
		if _, exists := headMap[path]; !exists {
			baseData, err := readBlobData(store.Objects, object.Hash(baseEntry.BlobHash))
			if err != nil {
				continue
			}
			fd, err := diff.DiffFiles(path, baseData, nil)
			if err != nil {
				continue
			}
			if len(fd.Changes) > 0 {
				fileDiffs = append(fileDiffs, fileDiffToResponse(fd))
			}
		}
	}

	return &DiffResponse{
		Base:  string(baseHash),
		Head:  string(headHash),
		Files: fileDiffs,
	}, nil
}

// --- helpers ---

func readBlobData(store *object.Store, h object.Hash) ([]byte, error) {
	blob, err := store.ReadBlob(h)
	if err != nil {
		return nil, err
	}
	return blob.Data, nil
}

func entityListToResponse(el *entity.EntityList) *EntityListResponse {
	entities := make([]EntityInfo, len(el.Entities))
	for i, e := range el.Entities {
		entities[i] = entityToInfo(&e)
	}
	return &EntityListResponse{
		Language: el.Language,
		Path:     el.Path,
		Entities: entities,
	}
}

var kindNames = map[entity.EntityKind]string{
	entity.KindPreamble:     "preamble",
	entity.KindImportBlock:  "import",
	entity.KindDeclaration:  "declaration",
	entity.KindInterstitial: "interstitial",
}

func entityToInfo(e *entity.Entity) EntityInfo {
	return EntityInfo{
		Kind:      kindNames[e.Kind],
		Name:      e.Name,
		DeclKind:  e.DeclKind,
		Receiver:  e.Receiver,
		StartLine: e.StartLine,
		EndLine:   e.EndLine,
		BodyHash:  e.BodyHash,
	}
}

var changeTypeNames = map[diff.ChangeType]string{
	diff.Added:    "added",
	diff.Removed:  "removed",
	diff.Modified: "modified",
}

func fileDiffToResponse(fd *diff.FileDiff) FileDiffResponse {
	changes := make([]EntityChangeInfo, len(fd.Changes))
	for i, c := range fd.Changes {
		change := EntityChangeInfo{
			Type: changeTypeNames[c.Type],
			Key:  c.Key,
		}
		if c.Before != nil {
			info := entityToInfo(c.Before)
			change.Before = &info
		}
		if c.After != nil {
			info := entityToInfo(c.After)
			change.After = &info
		}
		changes[i] = change
	}
	return FileDiffResponse{
		Path:    fd.Path,
		Changes: changes,
	}
}
