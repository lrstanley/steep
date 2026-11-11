// Post-build fixups and checks for the static site in dist/public, which is served from Cloudflare
// (https://getsteep.dev/).
//
//  1. The markdown outputs (llms.txt, llms-full.txt, /<page>.md) are generated with root-relative links
//     (e.g. /steep/harnesses). Rewrite links to other pages to the absolute URL of their markdown version
//     (/<page>.md, /index.md for the root), per https://llmstxt.org, so agents stay on markdown. Links to
//     anything else get the absolute site URL.
//  2. Validate every internal link, #fragment, and image in the generated HTML.
//  3. Check that every page, the 404 page included, has the go-import and go-source meta tags in its <head>, since
//     those are what resolve the getsteep.dev/steep Go import paths.
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { SECTIONS } from "../src/lib/sections";
import { GO_IMPORT, GO_SOURCE, SITE_URL } from "../src/lib/site";

const SITE = SITE_URL.replace(/\/$/, "");
const OUT = join(import.meta.dirname, "..", "dist", "public");

function walk(dir: string): string[] {
    return readdirSync(dir).flatMap((f) => {
        const p = join(dir, f);
        return statSync(p).isDirectory() ? walk(p) : [p];
    });
}

const files = walk(OUT);
const errors: string[] = [];

/** Maps a root-relative path (with an optional #fragment) to its absolute URL, preferring markdown. */
function toAbsolute(href: string, source: string) {
    const [path = "", fragment] = href.split("#");
    const page = path.replace(/\/$/, "") || "/index";
    const frag = fragment ? `#${fragment}` : "";
    if (existsSync(join(OUT, `${page}.md`))) return `${SITE}${page}.md${frag}`;
    if (!existsSync(join(OUT, path)) && !existsSync(join(OUT, path, "index.html"))) {
        errors.push(`${source}: no page or file for ${href}`);
    }
    return `${SITE}${path}${frag}`;
}

for (const file of files.filter((f) => f.endsWith(".md") || /llms(-full)?\.txt$/.test(f))) {
    const source = file.slice(OUT.length);
    const src = readFileSync(file, "utf8");
    const out = src
        // markdown links, [text](/path#frag)
        .replace(/\]\((\/(?!\/)[^)\s]*)\)/g, (_, href: string) => `](${toAbsolute(href, source)})`)
        // llms-full.txt page headings, "# Title (/path)"
        .replace(/^(# .+) \((\/[^)]*)\)$/gm, (_, title: string, href: string) => `${title} (${toAbsolute(href, source)})`);
    if (out !== src) writeFileSync(file, out);
}

const pageOrder = SECTIONS.flatMap((s) => {
    const index = readFileSync(join(OUT, s.path, "llms.txt"), "utf8");
    return [...index.matchAll(/\]\((https:[^)]+\.md)\)/g)].map((m) => m[1] as string);
});

// llms-full.txt comes out in file order. Put its pages in the order of the sidebars, so it reads from each section's
// introduction through to its reference.
const fullPath = join(OUT, "llms-full.txt");
const sections = readFileSync(fullPath, "utf8")
    .split(/^(?=# .+ \(https:[^)]+\)$)/m)
    .filter((s) => s.trim());
const rank = (s: string) => {
    const url = /^# .+ \((https:[^)]+)\)$/m.exec(s)?.[1] ?? "";
    const i = pageOrder.indexOf(url);
    return i < 0 ? pageOrder.length : i;
};
writeFileSync(fullPath, sections.sort((a, b) => rank(a) - rank(b)).map((s) => s.trimEnd()).join("\n\n") + "\n");

const htmlFiles = files.filter((f) => f.endsWith(".html"));
const ids = new Map<string, Set<string>>();
const idsOf = (file: string) => {
    let set = ids.get(file);
    if (!set) {
        set = new Set([...readFileSync(file, "utf8").matchAll(/\sid="([^"]+)"/g)].map((m) => m[1] as string));
        ids.set(file, set);
    }
    return set;
};

if (!existsSync(join(OUT, "404.html"))) errors.push("no 404.html, which go get needs for paths with no page");

/** The content of the <head> meta tag with the given name, or undefined. go get reads no further than the <head>. */
const headMeta = (html: string, name: string) => {
    const head = html.split(/<\/head>|<body[\s>]/i)[0] ?? "";
    for (const [tag] of head.matchAll(/<meta\s[^>]*>/gi)) {
        if (new RegExp(`\\sname="${name}"`).test(tag)) return /\scontent="([^"]*)"/.exec(tag)?.[1];
    }
    return undefined;
};

for (const file of htmlFiles) {
    const page = "/" + file.slice(OUT.length + 1).replace(/(^|\/)index\.html$/, "");
    const html = readFileSync(file, "utf8");
    for (const [name, want] of [
        ["go-import", GO_IMPORT],
        ["go-source", GO_SOURCE],
    ] as const) {
        const got = headMeta(html, name)?.replace(/&amp;/g, "&");
        if (got !== want) errors.push(`${page}: ${name} meta tag is ${got === undefined ? "missing" : `"${got}"`}, want "${want}"`);
    }
    for (const [, src = ""] of html.matchAll(/<img\s[^>]*?src="(\/demos\/[^"]*)"/g)) {
        if (!existsSync(join(OUT, src))) errors.push(`${page}: no file for image ${src}`);
    }
    for (const [, href = ""] of html.matchAll(/<a\s[^>]*?href="([^"]*)"/g)) {
        if (/^(https?:|mailto:|tel:|\/\/)/.test(href)) continue;

        const [path = "", fragment] = href.replace(/&amp;/g, "&").split("#");
        let target = file;
        if (path) {
            const rel = path.replace(/^\//, "").replace(/\/$/, "");
            const candidates = [join(OUT, rel, "index.html"), join(OUT, rel)];
            const found = candidates.find((c) => existsSync(c) && statSync(c).isFile());
            if (!found) {
                errors.push(`${page}: broken link ${href}`);
                continue;
            }
            target = found;
        }
        if (fragment && target.endsWith(".html") && !idsOf(target).has(decodeURIComponent(fragment))) {
            errors.push(`${page}: broken anchor ${href}`);
        }
    }
}

if (errors.length) {
    console.error(`\n${errors.length} problem(s):\n` + [...new Set(errors)].map((e) => `  - ${e}`).join("\n"));
    process.exit(1);
}
console.log(`[postbuild] checked links in ${htmlFiles.length} pages, pointed markdown links at ${SITE}`);
