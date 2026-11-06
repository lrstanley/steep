"use client";

import { useState } from "react";
import { demoSize } from "../lib/demos";

export function FlagPreviewClient({ on, off, onLabel, offLabel, alt }: { on: string; off: string; onLabel: string; offLabel: string; alt: string }) {
    const [enabled, setEnabled] = useState(true);
    return (
        <figure className="demo not-prose">
            <div className="steep-toggle mb-2" role="group" aria-label="Show the recording with or without the flag">
                <button type="button" aria-pressed={enabled} onClick={() => setEnabled(true)}>
                    {onLabel}
                </button>
                <button type="button" aria-pressed={!enabled} onClick={() => setEnabled(false)}>
                    {offLabel}
                </button>
            </div>
            <img src={`/demos/${enabled ? on : off}`} alt={`${alt}, with ${enabled ? onLabel : offLabel}`} loading="lazy" {...demoSize(enabled ? on : off)} />
        </figure>
    );
}
