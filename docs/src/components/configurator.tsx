import { asMarkdown, md } from "fumapress/markdown";
import { defaultConfig, formats, reelCommand, steepShell } from "../lib/export-config";
import { ConfiguratorClient } from "./configurator.client";

/** Dropdowns that write the commands of an export: for a test run, or for the reel CLI. */
export function Configurator() {
    if (asMarkdown()) {
        const rows = formats.map((f) => `| \`.${f.ext}\` | ${f.label} | ${f.ffmpeg ? "yes" : "no"} | ${f.params.map((p) => `\`${p.name}\` (${p.options.join(", ")})`).join(", ") || (f.stills ? "`match` (mark pattern)" : "none")} |`);
        return md`
The interactive configurator on this page builds the commands below from dropdowns. These are its defaults and the options it offers.

\`\`\`sh
${steepShell(defaultConfig())}
\`\`\`

\`\`\`sh
${reelCommand(defaultConfig())}
\`\`\`

| Output | Format | Needs ffmpeg | Parameters |
| --- | --- | --- | --- |
${rows.join("\n")}

`;
    }
    return <ConfiguratorClient />;
}
