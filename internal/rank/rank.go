// Package rank implements fractional indexing (LexoRank-style): string keys that
// sort lexicographically and always admit a new key strictly between any two, so
// items reorder without renumbering neighbours.
//
// Keys are generated only via Between/First/After/Before — never hand-set to the
// alphabet extremes, so there is always room to insert before the first / after
// the last key in use.
package rank

import "strings"

// digits must be ASCII-ascending; base36 keeps keys short and readable.
const digits = "0123456789abcdefghijklmnopqrstuvwxyz"

func idx(c byte) int { return strings.IndexByte(digits, c) }

// Between returns a key strictly between a and b (a < result < b lexicographically).
// Empty a means "before everything", empty b means "after everything".
// Callers must pass a < b (or an empty bound).
func Between(a, b string) string {
	var sb strings.Builder
	for i := 0; ; i++ {
		lo := 0
		if i < len(a) {
			lo = idx(a[i])
		}
		hi := len(digits)
		if i < len(b) {
			hi = idx(b[i])
		}
		if lo+1 < hi { // room for a midpoint char at this position
			sb.WriteByte(digits[(lo+hi)/2])
			return sb.String()
		}
		// no gap here: keep a's char (or the low extreme) and descend a position
		if i < len(a) {
			sb.WriteByte(a[i])
		} else {
			sb.WriteByte(digits[0])
		}
	}
}

// First returns a starting key (mid of the range), leaving room on both sides.
func First() string { return Between("", "") }

// After returns a key ordered after prev (append at the end).
func After(prev string) string { return Between(prev, "") }

// Before returns a key ordered before next (prepend at the start).
func Before(next string) string { return Between("", next) }

// Sequence returns n keys in ascending order — for bulk inserts / initial ranks.
func Sequence(n int) []string {
	keys := make([]string, 0, n)
	prev := ""
	for i := 0; i < n; i++ {
		prev = Between(prev, "")
		keys = append(keys, prev)
	}
	return keys
}
