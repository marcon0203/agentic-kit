package domain

// PageQuery is a keyset-pagination request. After is the *decoded* keyset
// value (an agent_ref, a numeric id as a string, ...) — cursor opacity is a
// transport concern, so internal/api base64-decodes an incoming cursor
// before calling a service and re-encodes NextCursor on the way out. The
// domain deals in the real key.
type PageQuery struct {
	Limit int
	After string
}

// Normalize clamps Limit into the platform's documented range. Services call
// this so a limit of 0 or 10_000 can't reach a repository, whatever the
// transport did or didn't validate.
func (q PageQuery) Normalize() PageQuery {
	const def, max = 20, 100
	switch {
	case q.Limit <= 0:
		q.Limit = def
	case q.Limit > max:
		q.Limit = max
	}
	return q
}

// Page is a slice of results plus the keyset needed to fetch the next one.
type Page[T any] struct {
	Items      []T
	HasMore    bool
	NextCursor string
}

// NewPage builds a Page from a repository result that was deliberately
// over-fetched by one row (limit+1) to detect a further page without a
// second COUNT query. nextKey extracts the keyset value from the last item
// actually returned.
//
// Items is normalized to a non-nil slice: the API contract requires `[]`
// rather than `null` for an empty list (架构设计文档 6.4), and guaranteeing
// it here means no handler can regress it.
func NewPage[T any](rows []T, limit int, nextKey func(T) string) Page[T] {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []T{}
	}
	page := Page[T]{Items: rows, HasMore: hasMore}
	if hasMore && len(rows) > 0 {
		page.NextCursor = nextKey(rows[len(rows)-1])
	}
	return page
}
