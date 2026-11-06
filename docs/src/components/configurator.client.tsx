"use client";

import { useState } from "react";
import { useCopyButton } from "fumadocs-ui/utils/use-copy-button";
import { defaultConfig, formats, notes, outputString, reelCommand, steepActions, steepGo, steepShell, type Config, type Effects, type Output } from "../lib/export-config";

type Target = "steep" | "cli";

const steepTabs = { Shell: steepShell, "GitHub Actions": steepActions, Go: steepGo };

const effectLabels: [keyof Effects, string][] = [
    ["keys", "Keys pressed"],
    ["pause", "Pause at the end"],
    ["cards", "Opening and closing cards"],
    ["mouse", "Mouse pointer"],
    ["toasts", "Notifications as toasts"],
];

function Toggle<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: readonly T[]; onChange: (v: T) => void }) {
    return (
        <div className="steep-toggle" role="group" aria-label={label}>
            {options.map((o) => (
                <button key={o} type="button" aria-pressed={value === o} onClick={() => onChange(o)}>
                    {o}
                </button>
            ))}
        </div>
    );
}

function Text({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string }) {
    return (
        <label>
            {label}
            <input type="text" value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
        </label>
    );
}

function Check({ label, value, onChange }: { label: string; value: boolean; onChange: (v: boolean) => void }) {
    return (
        <label>
            <input type="checkbox" checked={value} onChange={(e) => onChange(e.target.checked)} />
            {label}
        </label>
    );
}

export function ConfiguratorClient() {
    const [cfg, setCfg] = useState<Config>(defaultConfig);
    const [target, setTarget] = useState<Target>("steep");
    const [tab, setTab] = useState<keyof typeof steepTabs>("Shell");

    const set = (patch: Partial<Config>) => setCfg((c) => ({ ...c, ...patch }));
    const setOutput = (ext: string, patch: Partial<Output>) =>
        setCfg((c) => ({ ...c, outputs: { ...c.outputs, [ext]: { ...(c.outputs[ext] as Output), ...patch } } }));

    const code = target === "steep" ? steepTabs[tab](cfg) : reelCommand(cfg);

    const [copied, copy] = useCopyButton(() => navigator.clipboard.writeText(code));

    return (
        <section className="configurator steep-panel not-prose" aria-label="Export configurator">
            <Toggle label="What to export from" value={target === "steep" ? "A steep test run" : "The reel CLI"} options={["A steep test run", "The reel CLI"]} onChange={(v) => setTarget(v === "The reel CLI" ? "cli" : "steep")} />

            <fieldset>
                <legend>Outputs</legend>
                <div className="configurator-formats">
                    {formats.map((f) => {
                        const out = cfg.outputs[f.ext] as Output;
                        return (
                            <div key={f.ext} className="configurator-format">
                                <Check label={`.${f.ext}`} value={out.on} onChange={(on) => setOutput(f.ext, { on })} />
                                {out.on &&
                                    f.params.map((p) => (
                                        <label key={p.name}>
                                            {p.label}
                                            <select value={out.params[p.name] ?? ""} onChange={(e) => setOutput(f.ext, { params: { ...out.params, [p.name]: e.target.value } })}>
                                                <option value="">default</option>
                                                {p.options.map((o) => (
                                                    <option key={o}>{o}</option>
                                                ))}
                                            </select>
                                        </label>
                                    ))}
                                {out.on && f.stills && (
                                    <label>
                                        at marks
                                        <select value={out.match} onChange={(e) => setOutput(f.ext, { match: e.target.value })}>
                                            <option value="">the end only</option>
                                            <option value="*">every mark</option>
                                            <option value="input:*">each input</option>
                                            <option value="wait:*">each wait</option>
                                            <option value="failure:*">each failure</option>
                                        </select>
                                    </label>
                                )}
                                {out.on && <code className="text-xs text-fd-muted-foreground">{outputString(f, out, target === "steep" ? "{name}" : "out")}</code>}
                            </div>
                        );
                    })}
                </div>
            </fieldset>

            <div className="configurator-grid">
                {target === "steep" ? (
                    <fieldset>
                        <legend>Run</legend>
                        <label>
                            Record
                            <select value={cfg.mode} onChange={(e) => set({ mode: e.target.value as Config["mode"] })}>
                                <option value="failure">only failures</option>
                                <option value="on">always</option>
                            </select>
                        </label>
                        <Text label="Packages" value={cfg.packages} onChange={(packages) => set({ packages })} />
                        <Text label="-run" value={cfg.run} placeholder="TestLogin" onChange={(run) => set({ run })} />
                        <Text label="Directory" value={cfg.dir} placeholder="use -artifacts" onChange={(dir) => set({ dir })} />
                        <Check label="Put {status} in the file name" value={cfg.withStatus} onChange={(withStatus) => set({ withStatus })} />
                    </fieldset>
                ) : (
                    <fieldset>
                        <legend>Source</legend>
                        <label>
                            Record from
                            <select value={cfg.from} onChange={(e) => set({ from: e.target.value as Config["from"], input: { export: "recording.cast", run: "demo.tape", rec: "" }[e.target.value as Config["from"]] })}>
                                <option value="export">an asciicast file</option>
                                <option value="run">a tape</option>
                                <option value="rec">this terminal</option>
                            </select>
                        </label>
                        <Text label={{ export: "File", run: "Tape", rec: "Command" }[cfg.from]} value={cfg.input} placeholder={cfg.from === "rec" ? "$SHELL" : undefined} onChange={(input) => set({ input })} />
                    </fieldset>
                )}

                <fieldset>
                    <legend>Effects</legend>
                    {effectLabels.map(([key, label]) => (
                        <Check key={key} label={label} value={cfg.effects[key]} onChange={(on) => set({ effects: { ...cfg.effects, [key]: on } })} />
                    ))}
                </fieldset>

                <fieldset>
                    <legend>Look and timing</legend>
                    <Text label="FPS" value={cfg.fps} placeholder="20" onChange={(fps) => set({ fps })} />
                    <Text label="Speed" value={cfg.speed} placeholder="1" onChange={(speed) => set({ speed })} />
                    <Text label="Font" value={cfg.font} placeholder="JetBrains Mono" onChange={(font) => set({ font })} />
                    {target === "steep" && <Text label="Scale" value={cfg.scale} placeholder="1" onChange={(scale) => set({ scale })} />}
                </fieldset>

                {target === "steep" && (
                    <fieldset>
                        <legend>Debug data</legend>
                        <Check label="Record each message" value={cfg.messages} onChange={(messages) => set({ messages })} />
                        <Check label="Record the test's line" value={cfg.source} onChange={(source) => set({ source })} />
                        <Check label="Log each action" value={cfg.log} onChange={(log) => set({ log })} />
                    </fieldset>
                )}
            </div>

            <div className="grid gap-2">
                <div className="flex flex-wrap items-center justify-between gap-2">
                    {target === "steep" ? <Toggle label="Command format" value={tab} options={Object.keys(steepTabs) as (keyof typeof steepTabs)[]} onChange={setTab} /> : <span className="steep-eyebrow">Command</span>}
                    <button type="button" className="steep-chip cursor-pointer" onClick={copy}>
                        {copied ? "Copied" : "Copy"}
                    </button>
                </div>
                <pre>
                    <code>{code}</code>
                </pre>
            </div>

            <ul className="configurator-notes">
                {notes(cfg, target).map((n) => (
                    <li key={n}>{n}</li>
                ))}
            </ul>
        </section>
    );
}
