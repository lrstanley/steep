/** A shiki grammar for reel's tapes, the syntax of VHS: see /cli/tapes#tape-language. */
export const tapeLanguage = {
    name: "tape",
    scopeName: "source.tape",
    repository: {},
    patterns: [
        { name: "comment.line.number-sign", match: "#.*$" },
        { name: "string.quoted.double", begin: '"', end: '"', patterns: [{ name: "constant.character.escape", match: "\\\\." }] },
        { name: "string.quoted.single", begin: "'", end: "'" },
        { name: "string.quoted.other", begin: "`", end: "`" },
        { name: "string.regexp", match: "(?<=\\s)/[^/\\s][^/\\n]*/" },
        // A setting's name, after the command that sets it.
        {
            match: "\\b(Set|Env)(\\s+)([A-Za-z_][A-Za-z0-9_]*)",
            captures: { 1: { name: "keyword.control" }, 3: { name: "entity.name.function" } },
        },
        {
            name: "keyword.control",
            match: "\\b(Output|Require|Source|Type|Sleep|Hide|Show|Wait|Screenshot|Copy|Paste|Mark|Highlight|Zoom|Redact|Snapshot)\\b",
        },
        {
            name: "entity.name.function",
            match: "\\b(Enter|Space|Backspace|Delete|Insert|Tab|Escape|Up|Down|Left|Right|PageUp|PageDown|Home|End|ScrollUp|ScrollDown)\\b",
        },
        { name: "entity.name.function", match: "\\b(Ctrl|Alt|Shift)(?=\\+)" },
        // +Screen and +Line after Wait, and @80ms after Type or Wait.
        { name: "entity.name.function", match: "(?<=[A-Za-z])\\+(Screen|Line)\\b" },
        { name: "constant.numeric", match: "\\b\\d+(\\.\\d+)?(ns|us|ms|s|m|h|%)?\\b" },
        { name: "keyword.operator", match: "[@+]" },
    ],
};
