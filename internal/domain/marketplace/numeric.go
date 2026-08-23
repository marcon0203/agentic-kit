package marketplace

import "strconv"

// This context's keysets are numeric ids, but domain.PageQuery/Page carry
// the cursor as a string so one pagination type serves every context
// (agent's keyset is an agent_ref). These two helpers are that conversion,
// kept in one place instead of inline at each call site.

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// atoi parses a keyset value, treating anything unparseable — including the
// empty first-page cursor — as "start from the beginning". A malformed
// cursor is rejected at the transport boundary when it is base64-decoded,
// so reaching here with garbage would be a bug, not user input.
func atoi(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return id
}
