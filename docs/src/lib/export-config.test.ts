import { describe, expect, test } from "bun:test";
import { defaultConfig, formats, notes, outputString, reelCommand, steepActions, steepEnv, steepGo, steepShell } from "./export-config";

describe("outputString", () => {
    const format = (ext: string) => formats.find((f) => f.ext === ext)!;

    test("a path without options has no query", () => {
        expect(outputString(format("gif"), { on: true, params: {}, match: "" }, "{name}")).toBe("{name}.gif");
    });
    test("joins parameters and skips empty ones", () => {
        expect(outputString(format("gif"), { on: true, params: { "max-bytes": "5MB", loop: "once", optimize: "" }, match: "" }, "x")).toBe("x.gif?max-bytes=5MB&loop=once");
    });
    test("a still takes {mark} and match", () => {
        expect(outputString(format("png"), { on: true, params: {}, match: "input:*" }, "x")).toBe("x_{mark}.png?match=input:*");
    });
    test("a match on a recording format is left out", () => {
        expect(outputString(format("gif"), { on: true, params: {}, match: "input:*" }, "x")).toBe("x.gif");
    });
});

describe("steep", () => {
    test("the default records failures, kept by -artifacts", () => {
        expect(steepShell(defaultConfig())).toBe("STEEP_EXPORT_MODE='failure' \\\nSTEEP_EXPORT='{name}_{status}.gif,{name}_{status}.cast' \\\ngo test -artifacts ./...");
    });
    test("a directory replaces -artifacts", () => {
        const cfg = { ...defaultConfig(), dir: "out" };
        expect(steepShell(cfg)).toContain("STEEP_EXPORT_DIR='out' \\\ngo test ./...");
    });
    test("no outputs leaves STEEP_EXPORT unset", () => {
        const cfg = defaultConfig();
        for (const out of Object.values(cfg.outputs)) out.on = false;
        expect(steepEnv(cfg).map(([name]) => name)).toEqual(["STEEP_EXPORT_MODE"]);
    });
    test("CI uploads one directory, on failure or always by mode", () => {
        expect(steepActions(defaultConfig())).toContain("if: failure()");
        expect(steepActions({ ...defaultConfig(), mode: "on" })).toContain("if: always()");
        expect(steepActions(defaultConfig())).toContain("STEEP_EXPORT_DIR: \"${{ github.workspace }}/steep-artifacts\"");
    });
    test("Go sets the policy and effects", () => {
        const go = steepGo(defaultConfig());
        expect(go).toContain("steep.WithExportOnFailure(),");
        expect(go).toContain("effects.Keys(time.Second),");
        expect(go).toContain('reel.MustFiles("{name}_{status}.gif", "{name}_{status}.cast")');
    });
});

describe("steep Go", () => {
    test("no outputs leaves Outputs out, so the defaults apply", () => {
        const cfg = defaultConfig();
        for (const out of Object.values(cfg.outputs)) out.on = false;
        expect(steepGo(cfg)).not.toContain("Outputs");
    });
    test("scale and font share one Render", () => {
        expect(steepGo({ ...defaultConfig(), scale: "2", font: "Fira" })).toContain('Render: []render.Option{render.WithScale(2), render.WithFontFamilyName("Fira")},');
    });
});

describe("reel", () => {
    test("export reads a cast", () => {
        expect(reelCommand(defaultConfig())).toBe("reel export 'recording.cast' --no-cast-effects \\\n  -o 'out.gif' \\\n  -o 'out.cast' \\\n  --keys \\\n  --pause");
    });
    test("a path with a space stays one argument", () => {
        expect(reelCommand({ ...defaultConfig(), input: "tea shop.cast" })).toContain("reel export 'tea shop.cast'");
    });
    test("run names outputs after the tape", () => {
        expect(reelCommand({ ...defaultConfig(), from: "run", input: "demo.tape" })).toContain("-o '{tape}.gif'");
    });
    test("rec takes its command last", () => {
        const cmd = reelCommand({ ...defaultConfig(), from: "rec", input: "htop" });
        expect(cmd.startsWith("reel rec \\\n")).toBe(true);
        expect(cmd.endsWith("-- htop")).toBe(true);
    });
    test("rec keeps a command's own --", () => {
        expect(reelCommand({ ...defaultConfig(), from: "rec", input: "go run . -- -v" }).endsWith("-- go run . -- -v")).toBe(true);
    });
});

describe("notes", () => {
    test("name what needs ffmpeg", () => {
        const cfg = defaultConfig();
        cfg.outputs.webp!.on = true;
        expect(notes(cfg, "steep").join(" ")).toContain("WebP need ffmpeg");
    });
    test("warn that text outputs skip effects", () => {
        const cfg = defaultConfig();
        cfg.outputs.snap!.on = true;
        expect(notes(cfg, "cli").join(" ")).toContain("show what a redaction hides");
    });
});
