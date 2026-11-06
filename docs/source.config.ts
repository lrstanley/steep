import { defineConfig } from "fumadocs-mdx/config";
import { rehypeCodeDefaultOptions } from "fumadocs-core/mdx-plugins/rehype-code";
import { remarkMdxMermaid } from "fumadocs-core/mdx-plugins/remark-mdx-mermaid";
import { transformerMetaHighlight, transformerMetaWordHighlight } from "@shikijs/transformers";
import { steepDark, steepLight } from "./src/lib/code-theme";
import { tapeLanguage } from "./src/lib/tape-language";

export default defineConfig({
  mdxOptions: {
    remarkPlugins: (v) => [...v, remarkMdxMermaid],
    rehypeCodeOptions: {
      themes: { light: steepLight, dark: steepDark },
      langs: [tapeLanguage],
      transformers: [
        ...(rehypeCodeDefaultOptions.transformers ?? []),
        // supports ```go {1,3-5} and ```go /word/ meta strings.
        transformerMetaHighlight(),
        transformerMetaWordHighlight(),
      ],
    },
  },
});
