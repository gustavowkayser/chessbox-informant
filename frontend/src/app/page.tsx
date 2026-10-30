"use client";

import { Chessboard } from "./components/chessboard";
import { Notation } from "./components/notation";
import Panel from "./components/panel";

export default function Home() {
    return (
        <main className="h-dvh overflow-hidden p-5 flex gap-2">
            <div className="mx-auto flex h-full w-full max-w-[60dvh] flex-col gap-5">
                <div className="aspect-square w-full shrink-0">
                    <Chessboard />
                </div>
                <Notation />
            </div>
            <div className="w-full">
                <Panel />
            </div>
        </main>
    );
}
