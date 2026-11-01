package notation

import (
	"maps"

	"chessbox-informant/internal/chess"
)

// Convention is a language's piece letters. Figurines mean the same in every
// convention, so each one includes them.
type Convention struct {
	Name  string
	Names chess.PieceNames
}

var figurines = chess.PieceNames{
	"♔": chess.King, "♚": chess.King,
	"♕": chess.Queen, "♛": chess.Queen,
	"♖": chess.Rook, "♜": chess.Rook,
	"♗": chess.Bishop, "♝": chess.Bishop,
	"♘": chess.Knight, "♞": chess.Knight,
}

// Figurines returns the figurines alone, for a book whose letters are unknown.
func Figurines() chess.PieceNames { return maps.Clone(figurines) }

// Conventions lists the candidates, English first so that it wins ties.
var Conventions = []Convention{
	convention("english", "K", "Q", "R", "B", "N"),
	convention("portuguese", "R", "D", "T", "B", "C"),
	convention("spanish", "R", "D", "T", "A", "C"),
	convention("german", "K", "D", "T", "L", "S"),
	convention("french", "R", "D", "T", "F", "C"),
}

func convention(name, king, queen, rook, bishop, knight string) Convention {
	names := maps.Clone(figurines)
	names[king], names[queen], names[rook], names[bishop], names[knight] = chess.King, chess.Queen, chess.Rook, chess.Bishop, chess.Knight
	return Convention{Name: name, Names: names}
}
