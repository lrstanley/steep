import { asMarkdown, md } from "fumapress/markdown";

const changes = [4, 10, 16, 22, 28, 34, 40, 46, 52];

/** What WaitSettle waits for: the screen's changes along a line of time, then a whole quiet period without one. */
export function SettleDiagram() {
    if (asMarkdown()) {
        return md`A brew redraws the screen every 60ms, so with a 100ms settle period the wait restarts on each change. It returns once 100ms pass with no change.\n\n`;
    }
    return (
        <figure className="settle steep-panel not-prose" aria-label="The brew changes the screen often, so the quiet period only completes after its last change">
            <div className="settle-row">
                <span>screen changes</span>
                <div className="settle-track">
                    {changes.map((left) => (
                        <i key={left} style={{ left: `${left}%` }} />
                    ))}
                </div>
            </div>
            <div className="settle-row">
                <span>WaitSettle</span>
                <div className="settle-track">
                    <b style={{ left: "52%", width: "24%" }}>100ms quiet</b>
                    <i style={{ left: "76%", width: "3px" }} />
                </div>
            </div>
            <div className="settle-row">
                <span>WithTimeout</span>
                <div className="settle-track">
                    <i style={{ left: "98%", width: "3px", background: "var(--steep-fail)" }} />
                </div>
            </div>
        </figure>
    );
}
