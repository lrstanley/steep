import sizes from "../../fixtures/demo-sizes.json";

/** An image's size, which task docs:demos measures, so a page reserves its space while it loads. */
export function demoSize(src: string) {
    const [width, height] = (sizes as Record<string, number[]>)[src] ?? [];
    return { width, height };
}
