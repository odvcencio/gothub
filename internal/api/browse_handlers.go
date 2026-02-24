package api

import (
	"net/http"
	"strconv"
	"strings"
)

// GET /api/v1/repos/{owner}/{repo}/tree/{ref}/{path...}
func (s *Server) handleListTree(w http.ResponseWriter, r *http.Request) {
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
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")

	limit := 30
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	commits, err := s.browseSvc.ListCommits(r.Context(), owner, repo, ref, limit)
	if err != nil {
		jsonError(w, err.Error(), http.StatusNotFound)
		return
	}
	jsonResponse(w, http.StatusOK, commits)
}

// GET /api/v1/repos/{owner}/{repo}/commit/{hash}
func (s *Server) handleGetCommit(w http.ResponseWriter, r *http.Request) {
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

// GET /api/v1/repos/{owner}/{repo}/diff/{spec}
// spec is "base...head" where base and head are refs or commit hashes
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
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
