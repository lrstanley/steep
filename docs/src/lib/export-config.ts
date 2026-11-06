import data from "./export-formats.json";

// What the export configurator turns into commands. The formats and their query parameters are in
// export-formats.json, which steep's TestDocsExportFormats parses with reel.ParseFile, so they can't drift.

export type Format = (typeof data.formats)[number];
export const formats: Format[] = data.formats;

export type Output = {
    on: boolean;
    /** Query parameters, by name; absent or empty leaves the default. */
    params: Record<string, string>;
    /** For a still format, the marks to write one at, such as "input:*"; empty writes the end's. */
    match: string;
};

export type Effects = { keys: boolean; pause: boolean; cards: boolean; mouse: boolean; toasts: boolean };

export type Config = {
    outputs: Record<string, Output>;
    effects: Effects;
    fps: string;
    speed: string;
    font: string;
    // Steep.
    mode: "on" | "failure";
    withStatus: boolean;
    dir: string;
    packages: string;
    run: string;
    scale: string;
    messages: boolean;
    source: boolean;
    log: boolean;
    // The CLI.
    from: "export" | "run" | "rec";
    input: string;
};

export function defaultConfig(): Config {
    const outputs: Record<string, Output> = {};
    for (const f of formats) outputs[f.ext] = { on: f.ext === "gif" || f.ext === "cast", params: {}, match: "" };
    return {
        outputs,
        effects: { keys: true, pause: true, cards: false, mouse: false, toasts: false },
        fps: "",
        speed: "",
        font: "",
        mode: "failure",
        withStatus: true,
        dir: "",
        packages: "./...",
        run: "",
        scale: "",
        messages: false,
        source: false,
        log: false,
        from: "export",
        input: "recording.cast",
    };
}

function selected(cfg: Config) {
    return formats.filter((f) => cfg.outputs[f.ext]?.on);
}

/** One output, as reel.ParseFile reads it: a path with a query. */
export function outputString(f: Format, out: Output, base: string) {
    const query = Object.entries(out.params)
        .filter(([, value]) => value)
        .map(([name, value]) => `${name}=${value}`);
    const still = f.stills && out.match !== "";
    if (still) query.push(`match=${out.match}`);
    return `${base}${still ? "_{mark}" : ""}.${f.ext}${query.length ? `?${query.join("&")}` : ""}`;
}

function outputStrings(cfg: Config, base: string) {
    return selected(cfg).map((f) => outputString(f, cfg.outputs[f.ext] as Output, base));
}

function steepBase(cfg: Config) {
    return cfg.withStatus ? "{name}_{status}" : "{name}";
}

function quote(s: string) {
    return `'${s.replaceAll("'", `'\\''`)}'`;
}

/** The STEEP_* variables of a config, in the order the environment page lists them. */
export function steepEnv(cfg: Config): [string, string][] {
    const env: [string, string][] = [["STEEP_EXPORT_MODE", cfg.mode]];
    const outputs = outputStrings(cfg, steepBase(cfg));
    if (outputs.length) env.push(["STEEP_EXPORT", outputs.join(",")]);
    if (cfg.dir) env.push(["STEEP_EXPORT_DIR", cfg.dir]);
    if (cfg.fps) env.push(["STEEP_EXPORT_FPS", cfg.fps]);
    if (cfg.speed) env.push(["STEEP_EXPORT_SPEED", cfg.speed]);
    if (cfg.scale) env.push(["STEEP_RENDERER_SCALE", cfg.scale]);
    if (cfg.font) env.push(["STEEP_RENDERER_FONT_FAMILY", cfg.font]);
    if (cfg.messages) env.push(["STEEP_RECORD_MESSAGES", "on"]);
    if (cfg.source) env.push(["STEEP_SOURCE", "on"]);
    if (cfg.log) env.push(["STEEP_LOG", "on"]);
    return env;
}

function goTestArgs(cfg: Config, artifacts: boolean) {
    return ["go test", artifacts && "-artifacts", cfg.run && `-run ${quote(cfg.run)}`, cfg.packages || "./..."].filter(Boolean).join(" ");
}

export function steepShell(cfg: Config) {
    const env = steepEnv(cfg).map(([name, value]) => `${name}=${quote(value)} \\`);
    return [...env, goTestArgs(cfg, !cfg.dir)].join("\n");
}

export function steepActions(cfg: Config) {
    // A relative directory would be one per package, so the job writes to one absolute directory to upload.
    const env = steepEnv({ ...cfg, dir: "${{ github.workspace }}/steep-artifacts" });
    return [
        "- name: Test",
        "  run: " + goTestArgs(cfg, false),
        "  env:",
        ...env.map(([name, value]) => `    ${name}: ${JSON.stringify(value)}`),
        "- name: Upload recordings",
        "  uses: actions/upload-artifact@v7",
        `  if: ${cfg.mode === "failure" ? "failure()" : "always()"}`,
        "  with:",
        "    name: steep-recordings",
        "    path: steep-artifacts",
        "    if-no-files-found: ignore",
    ].join("\n");
}

function goEffects(fx: Effects) {
    return [
        fx.cards && "steep.Logo(2 * time.Second),",
        fx.keys && "effects.Keys(time.Second),",
        fx.mouse && "effects.Mouse(),",
        fx.toasts && "effects.Toasts(),",
        fx.pause && "effects.Pause(),",
        fx.cards && "steep.Outro(2 * time.Second),",
    ].filter(Boolean) as string[];
}

export function steepGo(cfg: Config) {
    const fx = goEffects(cfg.effects);
    const outputs = outputStrings(cfg, steepBase(cfg)).map((o) => JSON.stringify(o));
    const render = [cfg.scale && `render.WithScale(${cfg.scale})`, cfg.font && `render.WithFontFamilyName(${JSON.stringify(cfg.font)})`].filter(Boolean);
    const profile = [
        // Without Outputs, steep writes its defaults. An empty list would write nothing.
        outputs.length > 0 && `Outputs: reel.MustFiles(${outputs.join(", ")}),`,
        cfg.fps && `FPS: ${cfg.fps},`,
        cfg.speed && `Speed: ${cfg.speed},`,
        render.length > 0 && `Render: []render.Option{${render.join(", ")}},`,
        fx.length > 0 && ["Effects: []reel.Effect{", ...fx.map((e) => `\t${e}`), "},"].join("\n\t\t"),
    ].filter(Boolean);
    const options = [
        ["steep.WithRecording(reel.Profile{", ...profile.map((p) => `\t${p}`), "}),"].join("\n\t"),
        cfg.mode === "failure" ? "steep.WithExportOnFailure()," : "steep.WithExportAlways(),",
        cfg.dir && `steep.WithArtifacts(${JSON.stringify(cfg.dir)}),`,
        cfg.messages && "steep.WithRecordMessages(true),",
        cfg.source && "steep.WithSource(true),",
        cfg.log && "steep.WithLog(t),",
    ].filter(Boolean);
    return ["h := steep.NewHarness(t, newModel(),", ...options.map((o) => `\t${o}`), ")"].join("\n");
}

function cliBase(cfg: Config) {
    return cfg.from === "run" ? "{tape}" : "out";
}

export function reelCommand(cfg: Config) {
    const fx = cfg.effects;
    const lead = {
        // Without this, effects the file records apply on top of those chosen here.
        export: `reel export ${quote(cfg.input || "recording.cast")} --no-cast-effects`,
        run: `reel run ${quote(cfg.input || "demo.tape")}`,
        rec: "reel rec",
    }[cfg.from];
    const parts = [
        lead,
        ...outputStrings(cfg, cliBase(cfg)).map((o) => `-o ${quote(o)}`),
        cfg.fps && `--fps ${cfg.fps}`,
        cfg.speed && `--speed ${cfg.speed}`,
        cfg.font && `--font ${quote(cfg.font)}`,
        fx.cards && "--intro 2s --outro 2s --outro-line 'Thanks'",
        fx.keys && "--keys",
        fx.mouse && "--mouse",
        fx.toasts && "--toasts",
        fx.pause && "--pause",
        // reel rec takes its command after its flags.
        cfg.from === "rec" && cfg.input && `-- ${cfg.input}`,
    ];
    return parts.filter(Boolean).join(" \\\n  ");
}

/** What the reader should know about the commands of a config. */
export function notes(cfg: Config, target: "steep" | "cli") {
    const out: string[] = [];
    const chosen = selected(cfg);
    const ffmpeg = chosen.filter((f) => f.ffmpeg).map((f) => f.label);
    if (ffmpeg.length) out.push(`${ffmpeg.join(", ")} need ffmpeg on PATH; an export fails at once without it.`);
    if (!chosen.length) out.push(target === "steep" ? "With no outputs, steep writes {name}_{status}.gif and .cast." : "Choose at least one output.");
    if (target === "steep") {
        if (Object.values(cfg.effects).some(Boolean)) {
            out.push("Effects have no environment variable. The shell and CI tabs keep the effects your test's WithRecording sets; the Go tab sets them.");
        }
        if (!cfg.dir) out.push("Without a directory, go test -artifacts keeps the files. Without that flag, they are removed when the test ends.");
    }
    if (chosen.some((f) => f.ext === "snap" || f.ext === "json")) {
        out.push(".snap and .json outputs skip effects, so they show what a redaction hides.");
    }
    return out;
}
