// Writes fixtures/demo-sizes.json, the size of each image pages show, so they reserve its space while it loads.
// Sizes come from the files' headers, since ffprobe can't read animated WebP. Run by "task docs:demos".
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { extname, join } from "node:path";

const docs = join(import.meta.dirname, "..");
const demos = join(docs, "public", "demos");

function size(name: string): [number, number] {
    const b = readFileSync(join(demos, name));
    if (extname(name) === ".png") return [b.readUInt32BE(16), b.readUInt32BE(20)];
    if (b.toString("latin1", 12, 16) === "VP8X") return [b.readUIntLE(24, 3) + 1, b.readUIntLE(27, 3) + 1];
    throw new Error(`${name}: not a WebP with a VP8X header`);
}

const sizes = Object.fromEntries(
    readdirSync(demos)
        .filter((name) => [".png", ".webp"].includes(extname(name)))
        .sort()
        .map((name) => [name, size(name)]),
);
writeFileSync(join(docs, "fixtures", "demo-sizes.json"), `${JSON.stringify(sizes, null, 2).replace(/\[\s+(\d+),\s+(\d+)\s+\]/g, "[$1, $2]")}\n`);
