"use client";

import { useState, type CSSProperties } from "react";

type Style = { fg?: string; bg?: string; attrs?: number; underline?: number };
type Cell = { content?: string; style?: Style; width: number };

export type Screen = { cols: number; rows: number; fg_color: string; bg_color: string; cells: Cell[][] };

export function CellsClient({ screen }: { screen: Screen }) {
    const [at, setAt] = useState({ x: 0, y: 0 });
    const cell = screen.cells[at.y]?.[at.x];
    return (
        <div className="not-prose my-6 grid gap-3">
            <div className="cells overflow-x-auto" style={{ "--cols": screen.cols, "--cells-bg": screen.bg_color, color: screen.fg_color } as CSSProperties}>
                {screen.cells.flatMap((row, y) =>
                    row.map((c, x) => (
                        <button
                            key={`${x},${y}`}
                            type="button"
                            aria-current={at.x === x && at.y === y}
                            aria-label={`Cell ${x}, ${y}`}
                            onMouseEnter={() => setAt({ x, y })}
                            onFocus={() => setAt({ x, y })}
                            style={{ color: c.style?.fg, background: c.style?.bg, fontWeight: c.style?.attrs ? 700 : undefined }}
                        >
                            {c.content || " "}
                        </button>
                    )),
                )}
            </div>
            <p className="steep-eyebrow">
                cells[{at.y}][{at.x}]
            </p>
            <pre className="steep-panel overflow-x-auto p-3 font-mono text-[0.8125rem]">{JSON.stringify(cell, null, 2)}</pre>
        </div>
    );
}
