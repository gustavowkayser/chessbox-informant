import { Chess, type Square } from "chess.js";
import { create } from "zustand";

type ChessStore = {
    game: Chess;
    fen: string;
    history: string[];

    revision: number;
    refresh: () => void;
    getGame: (source: Chess) => Chess;
    
    makeMove: (from: string, to: string, promotion?: string) => boolean;
    undoMove: () => void;
    resetGame: () => void;
    loadFen: (fen: string) => void;
};

export const useChessStore = create<ChessStore>((set, get) => ({
    game: new Chess(),
    fen: new Chess().fen(),
    history: [],

    revision: 0,

    getGame: (source: Chess) => {
        const game = new Chess(get().fen);
        game.loadPgn(source.pgn());
        return game;
    },

    refresh: () => {
        set({ revision: get().revision + 1 });
    },

    makeMove: (from, to, promotion = "q") => {
        const game = get().getGame(get().game);

        try {
            game.move({ from, to, promotion });
        } catch {
            return false;
        }

        set({
            game,
            fen: game.fen(),
            history: game.history(),
        });

        return true;
    },

    undoMove: () => {
        const game = get().getGame(get().game);
        game.undo();

        set({
            game,
            fen: game.fen(),
            history: game.history(),
        });
    },

    resetGame: () => {
        const game = new Chess();

        set({
            game,
            fen: game.fen(),
            history: [],
        });
    },

    loadFen: (fen) => {
        try {
            const game = new Chess(fen);

            set({
                game,
                fen: game.fen(),
                history: [],
            });
        } catch {
            throw new Error("FEN inválido.");
        }
    },
}));
