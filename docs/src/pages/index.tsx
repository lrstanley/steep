import type { CSSProperties, ReactNode } from "react";
import { Link } from "fumapress/client";
import { asMarkdown, md } from "fumapress/markdown";
import { CopyInstall } from "../components/copy-install.client";
import { LlmsLinks } from "../components/llms-links";
import { Logo } from "../components/logo";
import { demoSize } from "../lib/demos";
import { INSPECTOR_URL, SHOWCASE_INSPECTOR_URL, SITE_URL, inspectorUrl } from "../lib/site";

const tagline = "Test what your terminal program actually draws.";

const summary =
    "steep runs your program in an emulated terminal, drives it with keys, text, mouse events, and resizes, and checks what's on the screen, in plain go test. When a test fails, it leaves you a recording of what happened.";

const noCgo = "No cgo required.";

const install = "go get getsteep.dev/steep@latest";

// The test in the hero, as its lines run, and the mark each leaves in the recording.
const testLines = [
    { code: "steep.NewHarness(t, newModel()).", mark: "ready:1" },
    { code: '\tWait("Sencha").', mark: "wait:1" },
    { code: "\tKeyDown().", mark: "input:1" },
    { code: '\tAssert("Matcha").', mark: "assert:1" },
    { code: '\tKey("enter").', mark: "input:2" },
    { code: '\tWait("Matcha is ready").', mark: "wait:2" },
    { code: "\tAssertSnapshot()", mark: "capture:1" },
];

const showcaseAbout =
    "A steep test of a dashboard written to push a terminal: live charts, a theme that fades the whole palette, and deploys that go live, wait for approval, and fail. The title bar's status, the progress bar, and the outline around the window are drawn from what the program told its terminal. The logo, the keys, and the PASS card are effects.";
const showcaseAlt =
    "A terminal dashboard recorded by a test: it boots, fades through its themes, then runs a deploy that waits for approval and goes live, and one that fails, with its status and an outline around the window changing with each";
const showcaseSource = "https://github.com/lrstanley/steep/blob/master/_examples/steep-showcase/main_test.go";

const versus = [
    { what: "Trying the main path", manual: "Run it, press keys, and look, before every release", steep: "Key, Wait, and Assert, in milliseconds, on every commit" },
    { what: "Window sizes", manual: "Drag the window around, if you remember", steep: "Resize(48, 16), then a snapshot of the result" },
    { what: "The mouse", manual: "Click and watch", steep: 'Locate(reel.Contains("Save")).LeftClick()' },
    { what: "A layout that shifted", manual: "Noticed by a user, a month later", steep: "A snapshot fails, with a diff" },
    { what: "A bug on someone's machine", manual: '"It just looked weird"', steep: "A video, and a file you can step through" },
];

type Section = {
    href: string;
    title: string;
    eyebrow: string;
    demo: string;
    alt: string;
    body: string;
    links: { text: string; href: string }[];
};

const sections: Section[] = [
    {
        href: "/steep",
        title: "Steep",
        eyebrow: "The testing library",
        demo: "fail-assert.webp",
        alt: "A test run that ends on a FAIL card listing its failures",
        body: "Test Bubble Tea models, components, or any command on a pseudo-terminal, with waits, assertions, text and JSON snapshots, and recordings of failures.",
        links: [
            { text: "Your first test", href: "/steep/first-test" },
            { text: "Add steep to an existing project", href: "/steep/existing-project" },
            { text: "Choosing a harness", href: "/steep/harnesses" },
            { text: "Waiting and settling", href: "/steep/checks#waiting-and-settling" },
            { text: "Snapshots", href: "/steep/snapshots" },
            { text: "Environment variables", href: "/steep/environment" },
        ],
    },
    {
        href: "/reel",
        title: "Reel",
        eyebrow: "The recording library",
        demo: "effect-highlight.webp",
        alt: "A recording with everything dimmed but one row",
        body: "Record terminals from Go, and export them to GIF, WebP, MP4, WebM, PNG, or asciicast, through a renderer and effects such as key overlays, zooms, and redaction.",
        links: [
            { text: "Your first recording", href: "/reel/first-recording" },
            { text: "Output formats", href: "/reel/formats" },
            { text: "Effects", href: "/reel/effects" },
            { text: "Window look", href: "/reel/rendering#window-look" },
            { text: "Export cookbook", href: "/reel/cookbook" },
        ],
    },
    {
        href: "/cli",
        title: "Reel CLI",
        eyebrow: "The same, without Go",
        demo: "cli-tape.webp",
        alt: "A demo recorded from a tape",
        body: "Run VHS tapes, test with them, record your own terminal, drive programs from scripts and agents, and turn asciicast files into GIFs and videos.",
        links: [
            { text: "Quickstart", href: "/cli/quickstart" },
            { text: "Make demos from tapes", href: "/cli/tapes" },
            { text: "Drive programs from scripts and agents", href: "/cli/sessions" },
            { text: "Appearance flags", href: "/cli/appearance" },
            { text: "All the commands", href: "/cli/commands/run" },
        ],
    },
];

const sampleCast = inspectorUrl("fail-wait.cast");

function Out({ href, className, children }: { href: string; className?: string; children: ReactNode }) {
    return (
        <a href={href} className={className} target="_blank" rel="noreferrer">
            {children}
        </a>
    );
}

export default function Page() {
    if (asMarkdown()) {
        return md`
# steep

${tagline}

${summary} **${noCgo}**

\`\`\`console
${install}
\`\`\`

> **Note**
>
> steep is pre-1.0. Its API may change in breaking ways until v1.0.0.

\`\`\`go
${testLines.map((l) => l.code).join("\n")}
\`\`\`

![A test of a small program, recorded by steep](${SITE_URL}demos/first-test.webp)

## Compared with testing by hand

${versus.map((v) => `- **${v.what}**: by hand, ${v.manual.charAt(0).toLowerCase()}${v.manual.slice(1)}. With steep: ${v.steep}.`).join("\n")}

## The showcase

${showcaseAbout}

![${showcaseAlt}](${SITE_URL}demos/showcase.webp)

- [Open it in the inspector](${SHOWCASE_INSPECTOR_URL})
- [Read the test](${showcaseSource})

## The documentation

${sections.map((s) => `### [${s.title}](${s.href})\n\n${s.eyebrow}. ${s.body}\n\n${s.links.map((l) => `- [${l.text}](${l.href})`).join("\n")}`).join("\n\n")}

## The inspector

The [inspector](${INSPECTOR_URL}) is a web page for stepping through the \`.cast\` files steep and reel write, event by event. It runs in your browser, and files never leave it.

## Links

- [Export configurator](/steep/configurator)
- [Source](https://github.com/lrstanley/steep)
- [API reference](https://pkg.go.dev/getsteep.dev/steep)
`;
    }

    return (
        <>
            <title>steep: test what your terminal program actually draws</title>
            <meta name="description" content={`${summary} ${noCgo}`} />
            <meta property="og:title" content="steep" />
            <meta property="og:description" content={tagline} />

            <main className="home">
                <section className="home-hero">
                    <div className="home-hero-inner">
                        <div>
                            <p className="steep-eyebrow">A test harness for terminal programs</p>
                            <h1>
                                <Logo large />
                            </h1>
                            <p className="home-tagline">{tagline}</p>
                            <p className="home-lede">
                                {summary} <strong>{noCgo}</strong>
                            </p>
                            <div className="home-actions">
                                <Link href="/steep/first-test" className="home-button home-button-primary">
                                    Write your first test
                                </Link>
                                <Link href="/steep/existing-project" className="home-button">
                                    I have a project already
                                </Link>
                                <Out href="https://github.com/lrstanley/steep" className="home-button">
                                    GitHub
                                </Out>
                            </div>
                            <CopyInstall text={install} />
                            <p className="home-notice">
                                <span className="steep-chip">Pre-1.0</span>
                                Expect breaking changes until v1.0.0.
                            </p>
                        </div>
                        <div className="home-hero-demo">
                            <img src="/demos/first-test.webp" alt="A test driving a small program: a key moves down its menu, enter brews a tea, and a card reads PASS" {...demoSize("first-test.webp")} />
                            <div className="home-code steep-panel">
                                <div className="home-code-bar">
                                    <span>main_test.go</span>
                                    <span className="home-pass">PASS</span>
                                </div>
                                {testLines.map((l, i) => (
                                    <div key={l.mark} className="home-code-line" style={{ "--step": i } as CSSProperties}>
                                        <span>{l.code.replace("\t", "    ")}</span>
                                        <span className="steep-chip">{l.mark}</span>
                                    </div>
                                ))}
                            </div>
                        </div>
                    </div>
                </section>

                <section className="home-section">
                    <div>
                        <h2>What you stop doing by hand</h2>
                        <p className="home-section-lede">
                            Most terminal programs are tested by running them and looking. A test does the same walk every
                            time, at sizes and in colors you wouldn't think to try. <Link href="/steep#why-test-your-tui" className="underline underline-offset-3">Read why</Link>.
                        </p>
                    </div>
                    <dl className="home-versus">
                        {versus.map((v) => (
                            <div key={v.what}>
                                <dt>{v.what}</dt>
                                <dd>{v.manual}</dd>
                                <dd>{v.steep}</dd>
                            </div>
                        ))}
                    </dl>
                </section>

                <section className="home-section home-showcase">
                    <div>
                        <p className="steep-eyebrow">The showcase</p>
                        <h2>One test, and everything a terminal can show</h2>
                        <p className="home-section-lede">
                            {showcaseAbout}
                        </p>
                        <div className="home-actions">
                            <Out href={SHOWCASE_INSPECTOR_URL} className="home-button home-button-primary">
                                Open it in the inspector
                            </Out>
                            <Out href={showcaseSource} className="home-button">
                                Read the test
                            </Out>
                        </div>
                    </div>
                    <img src="/demos/showcase.webp" alt={showcaseAlt} loading="lazy" {...demoSize("showcase.webp")} />
                </section>

                <section className="home-section">
                    <div>
                        <h2>Three tools, one recording format</h2>
                        <p className="home-section-lede">
                            steep is built on reel, and so is the CLI, so what you learn about formats and effects carries
                            across. Each has its own section, so you only read what you need.
                        </p>
                    </div>
                    <div className="home-cards">
                        {sections.map((s) => (
                            <article key={s.href} className="home-card steep-panel">
                                <p className="steep-eyebrow">{s.eyebrow}</p>
                                <h3>
                                    <Link href={s.href}>{s.title}</Link>
                                </h3>
                                <img src={`/demos/${s.demo}`} alt={s.alt} loading="lazy" {...demoSize(s.demo)} />
                                <p>{s.body}</p>
                                <ul>
                                    {s.links.map((l) => (
                                        <li key={l.href}>
                                            <Link href={l.href}>{l.text}</Link>
                                        </li>
                                    ))}
                                </ul>
                            </article>
                        ))}
                    </div>
                </section>

                <section className="home-section home-split">
                    <div>
                        <p className="steep-eyebrow">The inspector</p>
                        <h2>See why it failed, event by event</h2>
                        <p className="home-section-lede">
                            Open a recording and scrub through it. After any event, you see the screen, the terminal's state,
                            the escape sequences, and what the test did, down to the line. It runs in your browser, and files
                            never leave it.
                        </p>
                        <div className="home-actions">
                            <Out href={sampleCast} className="home-button home-button-primary">
                                Open a failing test
                            </Out>
                            <Link href="/steep/debugging#step-through-a-failure-in-the-inspector" className="home-button">
                                How to use it
                            </Link>
                        </div>
                    </div>
                    <Out href={sampleCast}>
                        <img src="/inspector.png" alt="The inspector showing a failing test: a timeline of events, the screen at the selected event, and the terminal's state" loading="lazy" width={1400} height={860} />
                    </Out>
                </section>

                <section className="home-section home-split">
                    <img src="/demos/effect-keys.webp" alt="A recording with the keys pressed overlaid in its corner" loading="lazy" {...demoSize("effect-keys.webp")} />
                    <div>
                        <p className="steep-eyebrow">The export configurator</p>
                        <h2>Record any test, without changing it</h2>
                        <p className="home-section-lede">
                            Pick your formats from a list, and get the environment variables, the CI step, the Go options, or
                            the <code>reel</code> command to match.
                        </p>
                        <code className="home-install">STEEP_EXPORT_MODE=failure go test -artifacts ./...</code>
                        <div className="home-actions">
                            <Link href="/steep/configurator" className="home-button home-button-primary">
                                Open the configurator
                            </Link>
                        </div>
                    </div>
                </section>

                <footer className="home-footer">
                    <div>
                        <span>© Liam Stanley.</span>
                        <span className="flex flex-wrap gap-4">
                            <Out href="https://github.com/lrstanley/steep">GitHub</Out>
                            <Out href="https://pkg.go.dev/getsteep.dev/steep">pkg.go.dev</Out>
                            <Out href={INSPECTOR_URL}>Inspector</Out>
                        </span>
                        <LlmsLinks />
                    </div>
                </footer>
            </main>
        </>
    );
}

export const getConfig = async () => ({ render: "static" });
