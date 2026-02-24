package api

import (
	"net/http"
	"strconv"

	"github.com/odvcencio/gothub/internal/service"
)

func (s *Server) handleSearchSymbols(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	selector := r.URL.Query().Get("q")

	if selector == "" {
		selector = "*"
	}

	results, err := s.codeIntelSvc.SearchSymbols(r.Context(), owner, repo, ref, selector)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []service.SymbolResult{}
	}
	jsonResponse(w, http.StatusOK, results)
}

func (s *Server) handleFindReferences(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	name := r.URL.Query().Get("name")

	if name == "" {
		jsonError(w, "name query parameter is required", http.StatusBadRequest)
		return
	}

	results, err := s.codeIntelSvc.FindReferences(r.Context(), owner, repo, ref, name)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []service.ReferenceResult{}
	}
	jsonResponse(w, http.StatusOK, results)
}

func (s *Server) handleCallGraph(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizeRepoRequest(w, r, false); !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	ref := r.PathValue("ref")
	symbol := r.URL.Query().Get("symbol")

	if symbol == "" {
		jsonError(w, "symbol query parameter is required", http.StatusBadRequest)
		return
	}

	depth := 3
	if d := r.URL.Query().Get("depth"); d != "" {
		v, err := strconv.Atoi(d)
		if err != nil || v <= 0 {
			jsonError(w, "invalid depth query parameter", http.StatusBadRequest)
			return
		}
		if v > 16 {
			jsonError(w, "depth query parameter exceeds maximum of 16", http.StatusBadRequest)
			return
		}
		depth = v
	}

	reverse := r.URL.Query().Get("reverse") == "true"

	result, err := s.codeIntelSvc.GetCallGraph(r.Context(), owner, repo, ref, symbol, depth, reverse)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}
