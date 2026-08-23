package api

import (
	"net/http"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// cursorAfterString decodes the opaque `cursor` query param into the raw
// keyset value a domain service expects. An absent cursor is the empty
// string (start from the beginning), not an error.
func cursorAfterString(r *http.Request) (string, error) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return "", nil
	}
	return decodeCursorString(raw)
}

// mapPage converts a domain.Page of entities into a page of wire DTOs,
// preserving the pagination metadata. Entities never reach the wire: the
// DTO is the contract, and the two are free to diverge.
func mapPage[E any, D any](page domain.Page[E], convert func(E) D) domain.Page[D] {
	items := make([]D, 0, len(page.Items))
	for _, e := range page.Items {
		items = append(items, convert(e))
	}
	return domain.Page[D]{Items: items, HasMore: page.HasMore, NextCursor: page.NextCursor}
}
