"use client";

import { useChessStore } from "../contexts/chess.context";

export function Notation() {
    const game = useChessStore((state) => state.game);
    const undo = useChessStore((state) => state.undoMove);
    const history = game.history({ verbose: true });

    const moves = history.map((move, index) => {
        return {
            number: ((index + 1) / 2) + 0.5,
            san: move.san,
        }
    });

    return (
        <div className="min-h-0 flex-1 px-2 flex flex-col">
            <div className="min-h-0 flex-1 overflow-y-auto flex flex-wrap content-start gap-x-2 font-mono">
                {moves.map((move) => (
                    <div
                        key={move.san + move.number}
                        className="text-sm flex hover:bg-gray-700 hover:cursor-pointer p-2 rounded-lg"
                    >
                        {move.number == Math.floor(move.number) && <span>{move.number}.</span>} <span>{move.san}</span>
                    </div>
                ))}
            </div>
            <div className="mt-5 shrink-0">
                <button className="w-full h-10 bg-blue-950 rounded-lg cursor-pointer" onClick={undo}>
                    <span>Undo</span>
                </button>
            </div>
        </div>
    );
}
