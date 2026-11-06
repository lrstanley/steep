import { asMarkdown, md } from "fumapress/markdown";
import { CellsClient, type Screen } from "./cells.client";

/** A JSON snapshot drawn as its cells: choosing one shows what the snapshot holds for it. */
export function Cells({ screen }: { screen: Screen }) {
    if (asMarkdown()) {
        return md`The first cell of this snapshot's ${screen.cols} by ${screen.rows} grid:\n\n\`\`\`json\n${JSON.stringify(screen.cells[0]?.[0], null, 2)}\n\`\`\`\n\n`;
    }
    return <CellsClient screen={screen} />;
}
