package chess

import (
	"strings"

	lib "github.com/corentings/chess/v2"
)

type Piece int

const (
	Pawn Piece = iota
	Knight
	Bishop
	Rook
	Queen
	King
	// UnknownPiece is a piece whose type could not be read, such as a chess
	// font glyph extracted as U+FFFD. It stands for any piece but a pawn.
	UnknownPiece
)

type Castle int

const (
	NoCastle Castle = iota
	ShortCastle
	LongCastle
)

// MoveSpec is a move as a book prints it, without check marks or glyphs,
// which books do not always get right.
type MoveSpec struct {
	// Text is the move as printed, for error messages.
	Text  string
	Piece Piece
	// From is the disambiguation: empty, a file, a rank or a square. For a
	// pawn capture it is the pawn's file.
	From    string
	To      string
	Capture bool
	// Promotion is the piece a pawn becomes; Pawn, the zero value, means none.
	Promotion Piece
	Castle    Castle
}

var libPieces = map[lib.PieceType]Piece{
	lib.Pawn:   Pawn,
	lib.Knight: Knight,
	lib.Bishop: Bishop,
	lib.Rook:   Rook,
	lib.Queen:  Queen,
	lib.King:   King,
}

// resolve finds the legal moves in pos that spec describes. More than one
// means the spec is ambiguous.
func resolve(pos *lib.Position, spec MoveSpec) []lib.Move {
	var matches []lib.Move
	for _, m := range pos.ValidMoves() {
		if matchesSpec(pos, m, spec) {
			matches = append(matches, m)
		}
	}
	return matches
}

func matchesSpec(pos *lib.Position, m lib.Move, spec MoveSpec) bool {
	switch spec.Castle {
	case ShortCastle:
		return m.HasTag(lib.KingSideCastle)
	case LongCastle:
		return m.HasTag(lib.QueenSideCastle)
	}
	if m.HasTag(lib.KingSideCastle) || m.HasTag(lib.QueenSideCastle) {
		return false
	}

	piece := libPieces[pos.Board().Piece(m.S1()).Type()]
	if spec.Piece == UnknownPiece {
		if piece == Pawn {
			return false
		}
	} else if piece != spec.Piece {
		return false
	}

	from := m.S1().String()
	switch len(spec.From) {
	case 1:
		if !strings.Contains(from, spec.From) {
			return false
		}
	case 2:
		if from != spec.From {
			return false
		}
	}

	capture := m.HasTag(lib.Capture) || m.HasTag(lib.EnPassant)
	if m.S2().String() != spec.To || capture != spec.Capture {
		return false
	}

	if m.Promo() == lib.NoPieceType {
		return spec.Promotion == Pawn
	}
	return spec.Promotion == UnknownPiece || libPieces[m.Promo()] == spec.Promotion
}
