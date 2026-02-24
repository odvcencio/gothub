package api

import "net/http"

func parsePagination(r *http.Request, defaultPerPage, maxPerPage int) (page, perPage int) {
	page = parsePositiveInt(r.URL.Query().Get("page"), 1)
	perPage = parsePositiveInt(r.URL.Query().Get("per_page"), defaultPerPage)
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return page, perPage
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	var n int
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return fallback
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return fallback
	}
	return n
}

func paginateSlice[T any](items []T, page, perPage int) []T {
	if perPage <= 0 {
		return items
	}
	start := (page - 1) * perPage
	if start >= len(items) {
		return []T{}
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
