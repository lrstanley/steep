import type { ReactNode } from "react";
import { asMarkdown, md } from "fumapress/markdown";
import { demoSize } from "../lib/demos";
import { inspectorUrl, SITE_URL } from "../lib/site";

/** A recording or still from public/demos, which task docs:demos writes. A cast opens it in the inspector. */
export function Demo({ src, alt, caption, cast }: { src: string; alt: string; caption?: string; cast?: string }) {
    const inspect = cast && inspectorUrl(cast);
    if (asMarkdown()) {
        return md`![${alt}](${SITE_URL}demos/${src})${caption ? `\n\n${caption}` : ""}${inspect ? `\n\n[Open the recording in the inspector](${inspect})` : ""}\n\n`;
    }
    return (
        <figure className="demo not-prose">
            <img src={`/demos/${src}`} alt={alt} loading="lazy" {...demoSize(src)} />
            {(caption || inspect) && (
                <figcaption>
                    <span>{caption}</span>
                    {inspect && (
                        <a href={inspect} target="_blank" rel="noreferrer" className="underline underline-offset-3">
                            Open in the inspector ↗
                        </a>
                    )}
                </figcaption>
            )}
        </figure>
    );
}

/** Demos side by side, to compare them. */
export function DemoGrid({ children }: { children: ReactNode }) {
    if (asMarkdown()) return md`${children}`;
    return <div className="demo-grid">{children}</div>;
}

const THEMES = ["Tokyo Night", "Gruvbox Dark", "Cyberpunk", "Nord", "Catppuccin Latte", "Rose Pine Dawn"];

/** One screen of ANSI colors under each of a varied set of themes. */
export function ThemeGrid() {
    return (
        <DemoGrid>
            {THEMES.map((theme) => (
                <Demo key={theme} src={`cli-theme-${theme.toLowerCase().replaceAll(" ", "-")}.png`} alt={`A shell's ANSI colors in the ${theme} theme`} caption={theme} />
            ))}
        </DemoGrid>
    );
}
