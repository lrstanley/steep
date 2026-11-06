"use client";

import { useCopyButton } from "fumadocs-ui/utils/use-copy-button";
import { Check, Copy } from "lucide-react";

/** The install command. Clicking anywhere on it copies it. */
export function CopyInstall({ text }: { text: string }) {
    const [copied, copy] = useCopyButton(() => navigator.clipboard.writeText(text));
    return (
        <button type="button" className="home-install" onClick={copy} aria-label={`Copy ${text}`}>
            <code>{text}</code>
            {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
        </button>
    );
}
