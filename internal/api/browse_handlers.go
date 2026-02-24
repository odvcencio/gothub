package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/odvcencio/gothub/internal/service"
)

// GET /api/v1/repos/{owner}/{repo}/branches
func (s *Server) handleListBranches(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")

	branches, err := s.browseSvc.ListBranches(r.Context(), owner, repo)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	if branches == nil {
		branches = []string{}
	}
	jsonResponse(w, http.StatusOK, branches)
}

// GET /api/v1/repos/{owner}/{repo}/tree/{ref}/{path...}
func (s *Server) handleListTree(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	dirPath := r.PathValue("path")

	entries, err := s.browseSvc.ListTree(r.Context(), owner, repo, ref, dirPath)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, entries)
}

// GET /api/v1/repos/{owner}/{repo}/blob/{ref}/{path...}
func (s *Server) handleGetBlob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	filePath := r.PathValue("path")

	blob, err := s.browseSvc.GetBlob(r.Context(), owner, repo, ref, filePath)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, blob)
}

// GET /api/v1/repos/{owner}/{repo}/commits/{ref}
func (s *Server) handleListCommits(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")

	page, perPage := parsePagination(r, 30, 200)
	limit := page * perPage
	if l := strings.TrimSpace(r.URL.Query().Get("limit")); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 {
			jsonError(w, "invalid limit query parameter", http.StatusBadRequest)
			return
		}
		perPage = n
		limit = page * perPage
	}

	commits, err := s.browseSvc.ListCommits(r.Context(), owner, repo, ref, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, paginateSlice(commits, page, perPage))
}

// GET /api/v1/repos/{owner}/{repo}/commit/{hash}
func (s *Server) handleGetCommit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	hash := r.PathValue("hash")

	commit, err := s.browseSvc.GetCommit(r.Context(), owner, repo, hash)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, commit)
}

// GET /api/v1/repos/{owner}/{repo}/entities/{ref}/{path...}
func (s *Server) handleListEntities(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	filePath := r.PathValue("path")

	entities, err := s.diffSvc.ExtractEntities(r.Context(), owner, repo, ref, filePath)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, entities)
}

// GET /api/v1/repos/{owner}/{repo}/entity-history/{ref}?stable_id=...&name=...&body_hash=...
func (s *Server) handleEntityHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	stableID := strings.TrimSpace(r.URL.Query().Get("stable_id"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	bodyHash := strings.TrimSpace(r.URL.Query().Get("body_hash"))

	page, perPage := parsePagination(r, 50, 200)
	if l := strings.TrimSpace(r.URL.Query().Get("limit")); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 {
			jsonError(w, "invalid limit query parameter", http.StatusBadRequest)
			return
		}
		perPage = n
	}
	limit := page * perPage
	hits, err := s.diffSvc.EntityHistory(r.Context(), owner, repo, ref, stableID, name, bodyHash, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		} else if strings.Contains(err.Error(), "resolve ref") {
			status = http.StatusNotFound
		}
		jsonError(w, err.Error(), status)
		return
	}
	jsonResponse(w, http.StatusOK, paginateSlice(hits, page, perPage))
}

// GET /api/v1/repos/{owner}/{repo}/entity-log/{ref}?key=...&path=...&limit=...
func (s *Server) handleEntityLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			jsonError(w, "invalid limit query parameter", http.StatusBadRequest)
			return
		}
		if n > 500 {
			n = 500
		}
		limit = n
	}
	if key == "" {
		jsonError(w, "key query is required", http.StatusBadRequest)
		return
	}

	hits, err := s.diffSvc.EntityLog(r.Context(), owner, repo, ref, path, key, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "resolve ref") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		}
		jsonError(w, err.Error(), status)
		return
	}
	jsonResponse(w, http.StatusOK, hits)
}

// GET /api/v1/repos/{owner}/{repo}/entity-blame/{ref}?key=...&path=...&limit=...
func (s *Server) handleEntityBlame(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			jsonError(w, "invalid limit query parameter", http.StatusBadRequest)
			return
		}
		if n > 2000 {
			n = 2000
		}
		limit = n
	}
	if key == "" {
		jsonError(w, "key query is required", http.StatusBadRequest)
		return
	}

	blame, err := s.diffSvc.EntityBlame(r.Context(), owner, repo, ref, path, key, limit)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrEntityNotFound) {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "resolve ref") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		}
		jsonError(w, err.Error(), status)
		return
	}
	jsonResponse(w, http.StatusOK, blame)
}

// GET /api/v1/repos/{owner}/{repo}/diff/{spec}
// spec is "base...head" where base and head are refs or commit hashes
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	spec := r.PathValue("spec")

	parts := strings.SplitN(spec, "...", 2)
	if len(parts) != 2 {
		jsonError(w, "diff spec must be base...head", http.StatusBadRequest)
		return
	}

	result, err := s.diffSvc.DiffRefs(r.Context(), owner, repo, parts[0], parts[1])
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

// GET /api/v1/repos/{owner}/{repo}/semver/{spec}
// spec is "base...head" where base and head are refs or commit hashes.
func (s *Server) handleSemver(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	spec := r.PathValue("spec")

	parts := strings.SplitN(spec, "...", 2)
	if len(parts) != 2 {
		jsonError(w, "semver spec must be base...head", http.StatusBadRequest)
		return
	}

	result, err := s.diffSvc.RecommendSemver(r.Context(), owner, repo, parts[0], parts[1])
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}
