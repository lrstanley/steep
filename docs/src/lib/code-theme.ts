// The code palette of steep's brand (lrstanley/remotion, design/steep.md).
function theme(name: string, type: "dark" | "light", c: Record<"bg" | "id" | "kw" | "fn" | "str" | "punct" | "muted", string>) {
    return {
        name,
        type,
        colors: { "editor.background": c.bg, "editor.foreground": c.id },
        settings: [
            { settings: { background: c.bg, foreground: c.id } },
            { scope: ["comment", "punctuation.definition.comment"], settings: { foreground: c.muted, fontStyle: "italic" } },
            { scope: ["keyword", "storage", "storage.type", "keyword.operator.word"], settings: { foreground: c.kw } },
            { scope: ["entity.name.function", "support.function", "entity.name.tag", "support.type.property-name"], settings: { foreground: c.fn } },
            { scope: ["string", "constant.numeric", "constant.language", "constant.character"], settings: { foreground: c.str } },
            { scope: ["entity.name.type", "support.type", "entity.other.attribute-name"], settings: { foreground: c.fn } },
            { scope: ["punctuation", "keyword.operator", "meta.brace"], settings: { foreground: c.punct } },
        ],
    };
}

export const steepDark = theme("steep-dark", "dark", {
    bg: "#0b150f",
    id: "#cfdfc6",
    kw: "#e3b86b",
    fn: "#b2df7c",
    str: "#e9c98c",
    punct: "#8ea58c",
    muted: "#7d957a",
});

export const steepLight = theme("steep-light", "light", {
    bg: "#f3f7ec",
    id: "#1d3324",
    kw: "#9a5d12",
    fn: "#2f6f2a",
    str: "#8a5a14",
    punct: "#566c58",
    muted: "#4d6650",
});
