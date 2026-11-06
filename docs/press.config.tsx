import { type AppShape, defineConfig, type PressPlugin } from "fumapress";
import { fumadocsMdx } from "fumapress/adapters/mdx";
import { metaSchema, pageSchema } from "fumapress/adapters/mdx/schema";
import { createDocsLayoutPage } from "fumapress/layouts/docs";
import { createHomeLayout } from "fumapress/layouts/home";
import { createRootLayout } from "fumapress/layouts/root";
import { flexsearchPlugin } from "fumapress/plugins/flexsearch";
import { llmsPlugin } from "fumapress/plugins/llms.txt";
import { llms } from "fumadocs-core/source/llms";
import { defineDocs } from "fumadocs-mdx/macro";
import favicon from "./src/assets/icon.svg";
import { DiscordIcon } from "./src/components/discord-icon";
import { LlmsLinks } from "./src/components/llms-links";
import { Logo } from "./src/components/logo";
import { getMdxComponents } from "./src/components/mdx";
import { SearchDialog } from "./src/components/search-dialog.client";
import { mainLlmsTxt, SECTIONS, sectionLlmsTxt } from "./src/lib/sections";
import { INSPECTOR_URL, SITE_URL } from "./src/lib/site";

const docs = defineDocs({
    dir: "content",
    docs: {
        async: true,
        schema: pageSchema,
        lastModified: true,
        postprocess: {
            includeProcessedMarkdown: { output: "function" },
        },
    },
    meta: {
        schema: metaSchema,
    },
});

/** A plugin, without one of the routes it creates. */
function withoutRoute<C extends AppShape>(plugin: PressPlugin<C>, path: string): PressPlugin<C> {
    return {
        ...plugin,
        createPages(fns) {
            return plugin.createPages?.call(this, {
                ...fns,
                createApiIsomorphic: (route) => {
                    if (route.path !== path) fns.createApiIsomorphic(route);
                },
            });
        },
    };
}

const config = defineConfig({
    content: docs.toFumadocsSource(),
    mode: "static",
    site: {
        name: "steep",
        baseUrl: SITE_URL,
        git: {
            user: "lrstanley",
            repo: "steep",
            branch: "master",
            rootDir: "..",
        },
    },
    defaultLayoutProps: {
        nav: {
            title: <Logo />,
        },
        githubUrl: "https://github.com/lrstanley/steep",
        links: [
            { text: "Steep", url: "/steep", active: "nested-url" },
            { text: "Reel", url: "/reel", active: "nested-url" },
            { text: "CLI", url: "/cli", active: "nested-url" },
            { text: "Inspector", url: INSPECTOR_URL, external: true },
            {
                type: "icon",
                text: "Discord",
                label: "Discord",
                url: "https://liam.sh/chat",
                icon: <DiscordIcon />,
                external: true,
            },
        ],
    },
    renderRoot: createRootLayout({ providerProps: { search: { enabled: true, SearchDialog } } }),
    renderPage: createDocsLayoutPage({
        render(page) {
            return {
                layoutProps: { sidebar: { footer: <LlmsLinks section={page.url.split("/")[1]} className="mt-3 px-1" /> } },
            };
        },
        // The sidebar's dropdown already switches between the sections, which the home page has as links.
        renderLayout({ props, next }) {
            const internal = SECTIONS.map((s) => `/${s.path}`);
            return next({ ...props, links: props.links?.filter((l) => !("url" in l) || !internal.includes(l.url ?? "")) });
        },
    }),
    meta: {
        root() {
            return (
                <>
                    <meta name="author" content="Liam Stanley" />
                    <meta name="copyright" content="© Liam Stanley" />
                    <meta name="theme-color" content="#070c09" />
                    <meta name="darkreader-lock" />
                    <meta property="og:image" content={`${SITE_URL}og.png`} />
                    <meta property="og:image:width" content="1280" />
                    <meta property="og:image:height" content="640" />
                    <meta name="twitter:card" content="summary_large_image" />
                    <link rel="icon" href={favicon} type="image/svg+xml" />
                    <link rel="alternate icon" href="/favicon.ico" />
                    <link rel="apple-touch-icon" href="/apple-touch-icon.png" />
                    <link rel="preconnect" href="https://fonts.googleapis.com" />
                    <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="" />
                    <link
                        href="https://fonts.googleapis.com/css2?family=Geist+Mono:wght@400;500;700&family=Geist:wght@300..700&family=Tiny5&display=swap"
                        rel="stylesheet"
                    />
                </>
            );
        },
    },
})
    .adapters(fumadocsMdx({ getMdxComponents }))
    .plugins(
        // Its llms.txt lists every page. Ours, below, says which section to read.
        withoutRoute(llmsPlugin({ routes: "all" }), "/llms.txt"),
        flexsearchPlugin({
            // As the plugin's default does, with the section as the tag the search dialog filters by.
            async buildIndex(page) {
                for (const adapter of this.adapters) {
                    const structuredData = await adapter["core:get-structured-data"]?.call(this, page);
                    if (structuredData !== undefined) {
                        return {
                            id: page.url,
                            title: page.data.title ?? page.path,
                            description: page.data.description,
                            url: page.url,
                            tag: page.url.split("/")[1],
                            structuredData,
                        };
                    }
                }
                throw new Error(`no structured data for ${page.url}`);
            },
        }),
        {
            // Each section's own llms.txt.
            name: "steep:sections",
            createPages({ createApiIsomorphic }) {
                createApiIsomorphic({ path: "/llms.txt", handler: async () => new Response(mainLlmsTxt) });
                for (const section of SECTIONS) {
                    createApiIsomorphic({
                        path: `/${section.path}/llms.txt`,
                        handler: async () => {
                            const loader = await this.getLoader();
                            const { indexNode } = llms(loader);
                            const nodes = loader
                                .getPageTree()
                                .children.flatMap((n) => (n.type === "folder" && n.name === section.title ? [...(n.index ? [n.index] : []), ...n.children] : []));
                            if (!nodes.length) throw new Error(`llms.txt: no pages for section ${section.path}`);
                            return new Response(sectionLlmsTxt(section, nodes.map((n) => indexNode(n)).join("\n")));
                        },
                    });
                }
            },
        },
    );

export default config;
export const HomeLayout = createHomeLayout<typeof config.$context>();
