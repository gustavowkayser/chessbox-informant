"use client";

import {
    PieceDropHandlerArgs,
    Chessboard as ReactChessboard,
} from "react-chessboard";
import { useChessStore } from "../contexts/chess.context";

export function Chessboard() {
    const fen = useChessStore((state) => state.fen);
    const makeMove = useChessStore((state) => state.makeMove);

    return (
        <ReactChessboard
            options={{
                position: fen,
                onPieceDrop: ({
                    sourceSquare,
                    targetSquare,
                }: PieceDropHandlerArgs) => {
                    if (!targetSquare) return false;

                    return makeMove(sourceSquare, targetSquare);
                },
            }}
        />
    );
}
