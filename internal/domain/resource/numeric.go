package resource

import "strconv"

// itoa renders a numeric keyset value for domain.Page's string cursor.
func itoa(id int64) string { return strconv.FormatInt(id, 10) }
