import { asMarkdown, md } from "fumapress/markdown";
import { SITE_URL } from "../lib/site";
import { FlagPreviewClient } from "./flag-preview.client";

/** A flag's effect on a recording: buttons swap between what it writes with and without the flag. */
export function FlagPreview({ on, off, onLabel, offLabel, alt }: { on: string; off: string; onLabel: string; offLabel: string; alt: string }) {
    if (asMarkdown()) {
        return md`- \`${onLabel}\`: ![${alt}](${SITE_URL}demos/${on})\n- \`${offLabel}\`: ![${alt}](${SITE_URL}demos/${off})\n\n`;
    }
    return <FlagPreviewClient on={on} off={off} onLabel={onLabel} offLabel={offLabel} alt={alt} />;
}
