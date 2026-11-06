import { asMarkdown, md } from "fumapress/markdown";
import { formats } from "../lib/export-config";

const uses: Record<string, string> = {
    gif: "Anywhere an image goes",
    webp: "Web pages. It's the smallest image that animates.",
    mp4: "Social media, chat",
    webm: "Web video, with sharper colored text",
    cast: "Keeping the recording, the inspector, asciinema players",
    png: "A screenshot of the last frame, or one at each mark",
    snap: "The screen's text, for reading or comparing",
    json: "The screen's cells and state, for comparing or tools",
};

function humanSize(bytes: number) {
    return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`;
}

/** Every format, with the real size task docs:demos measured for one recording. */
export function FormatTable({ sizes }: { sizes: Record<string, number> }) {
    const rows = formats.map((f) => ({ ...f, size: humanSize(sizes[f.ext] ?? 0), use: uses[f.ext] ?? "" }));
    if (asMarkdown()) {
        return md`| Output | Size | Needs ffmpeg | Good for |\n| --- | --- | --- | --- |\n${rows.map((r) => `| \`.${r.ext}\` | ${r.size} | ${r.ffmpeg ? "yes" : "no"} | ${r.use} |`).join("\n")}\n\n`;
    }
    return (
        <div className="overflow-x-auto">
            <table>
                <thead>
                    <tr>
                        <th>Output</th>
                        <th>Size</th>
                        <th>Needs ffmpeg</th>
                        <th>Good for</th>
                    </tr>
                </thead>
                <tbody>
                    {rows.map((r) => (
                        <tr key={r.ext}>
                            <td>
                                <code>.{r.ext}</code>
                            </td>
                            <td>{r.size}</td>
                            <td>{r.ffmpeg ? "Yes" : "No"}</td>
                            <td>{r.use}</td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}
