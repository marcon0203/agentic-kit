package operation

import (
	"math"
	"strconv"

	"github.com/marcon0203/agentic-kit/internal/domain"
)

// Both listings in this context are newest-first, so their keyset runs
// downwards: the cursor is "everything with an id below this one", and an
// absent cursor starts at the top rather than at zero.
func descendingCursor(after string) int64 {
	if after == "" {
		return math.MaxInt64
	}
	id, err := strconv.ParseInt(after, 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return id
}

func newDescendingPage[T any](rows []T, limit int, keyOf func(T) int64) domain.Page[T] {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page := domain.Page[T]{Items: rows, HasMore: hasMore}
	if page.Items == nil {
		page.Items = []T{}
	}
	if hasMore {
		page.NextCursor = itoa(keyOf(rows[len(rows)-1]))
	}
	return page
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
