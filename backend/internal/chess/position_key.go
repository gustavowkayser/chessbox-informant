package chess

import "strings"

const keyFields = 3

// PositionKey keeps the placement, side to move and castling fields of a FEN.
// The FEN is not validated: a malformed one gives a key that matches nothing.
// Only ASCII whitespace separates fields, so the TypeScript implementation can
// match this one exactly.
func PositionKey(fen string) string {
	fields := strings.FieldsFunc(fen, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	return strings.Join(fields[:min(len(fields), keyFields)], " ")
}
