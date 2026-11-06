import { INSPECTOR_URL } from "./site";

/** The site's three sections, which stand alone: what llms.txt and the search filter offer. */
export const SECTIONS = [
    {
        path: "steep",
        title: "Steep",
        summary:
            "steep is a Go test harness for terminal programs. It runs a Bubble Tea model or component, or any command on a pseudo-terminal, in an emulated terminal, drives it with keys, text, mouse events, and resizes, and asserts on what it draws, with waits, text and JSON snapshots, and recordings of failures.",
        api: "https://pkg.go.dev/github.com/lrstanley/steep",
    },
    {
        path: "reel",
        title: "Reel",
        summary:
            "reel is the Go library steep records through. It emulates a terminal, logs everything that happens to it in a timeline saved as asciicast, and exports timelines to GIF, MP4, WebM, WebP, PNG, screen text, and JSON, through a renderer and effects such as key overlays, zooms, highlights, and redaction.",
        api: "https://pkg.go.dev/github.com/lrstanley/steep/reel",
    },
    {
        path: "cli",
        title: "Reel CLI",
        summary:
            "The reel command records terminals and renders recordings without Go. It runs VHS tapes, tests with tapes, records commands, drives programs in detached sessions for scripts and agents, and turns asciicast files into GIFs, videos, and stills.",
    },
];

/**
 * A section's own llms.txt, from the index of its pages ("- Title: description"). Each sidebar group becomes a heading,
 * as https://llmstxt.org lays them out.
 */
export function sectionLlmsTxt(section: (typeof SECTIONS)[number], index: string) {
    const pages = index
        .replace(/^- \*\*(.+)\*\*$/gm, "## $1\n")
        .replace(/\]\((\/[^)]*)\)/g, "]($1.md)")
        .trim();
    return `# ${section.title}

> ${section.summary}

This is one section of the documentation for steep, reel, and the reel CLI. [The overview](/llms.txt) says which to use.

${pages}

## Optional

${section.api ? `- [API reference](${section.api}): every package, type, and function\n` : ""}- [Everything](/llms-full.txt): every page of the site, as one file
`;
}

/** The site's llms.txt: what the three are, and which to read for what. Pages are served as markdown at <page>.md. */
export const mainLlmsTxt = `# steep

> Tools for testing and recording terminal programs (TUIs). steep is a Go test harness, reel is the Go library it records through, and the reel CLI does what reel does without writing Go. All three share one terminal emulator, one recording format (asciicast, as .cast files), and the same output formats and effects.

Choose by what you're doing, then read that section's llms.txt. Each section stands alone and lists its pages as markdown, so you only need the one. All three are pre-1.0, so their APIs may change between versions.

## Which to use

- **Writing or debugging Go tests of a terminal program: [Steep](/steep/llms.txt).** Use it for tests of a Bubble Tea model or component, or of any command on a pseudo-terminal, along with waits, assertions, snapshots, and recordings of failures.
- **Recording or rendering a terminal from Go, outside a test: [Reel](/reel/llms.txt).** Use it to generate demos, build tools on recordings, or write your own effects. Go tests should use steep, which adds waits, assertions, and failure handling on top of reel.
- **Anything without writing Go: [Reel CLI](/cli/llms.txt).** Use it for scripted demos from VHS tapes, black-box tests of a program in any language, recording your own terminal, and rendering or cutting .cast files.
- **An agent that needs to use a terminal program itself: [\`reel session\`](/cli/sessions.md).** It runs a program in a detached terminal and drives it one command at a time, reading the screen back as text, JSON, or an image. It needs no code.
- **Finding out what happened in a recording: [the inspector](${INSPECTOR_URL}).** It steps through a .cast file event by event. [\`reel info\`](/cli/commands/info.md) summarizes one from the command line.

## Install

\`\`\`console
go get github.com/lrstanley/steep@latest               # steep, which brings in reel
go get github.com/lrstanley/steep/reel@latest          # reel alone
go install github.com/lrstanley/steep/cmd/reel@latest  # the reel CLI
\`\`\`

## Documentation

${SECTIONS.map((s) => `- [${s.title}](/${s.path}/llms.txt): ${s.summary}`).join("\n")}

Every page is also available as markdown, at its path plus \`.md\`, such as [/steep/first-test.md](/steep/first-test.md).

## Optional

- [Overview](/index.md): the home page, comparing the three
- [Everything](/llms-full.txt): every page of the site, as one file
- [Source and examples](https://github.com/lrstanley/steep): the repository, including runnable programs in _examples
`;
