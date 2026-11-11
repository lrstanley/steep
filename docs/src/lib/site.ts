/** Where the site is published, with a trailing slash. Pages are served from its root. */
export const SITE_URL = "https://getsteep.dev/";

/** The hosted inspector. */
export const INSPECTOR_URL = "https://inspector.getsteep.dev/";

/** Opens a cast from public/casts in the hosted inspector. */
export function inspectorUrl(cast: string) {
    return `${INSPECTOR_URL}?url=${encodeURIComponent(`${SITE_URL}casts/${cast}`)}`;
}

/** Opens the sample cast the inspector itself serves, of the showcase example. */
export const SHOWCASE_INSPECTOR_URL = `${INSPECTOR_URL}?url=${encodeURIComponent(`${INSPECTOR_URL}samples/showcase.cast`)}`;

/** The repository the Go modules are fetched from. */
export const REPO_URL = "https://github.com/lrstanley/steep";

/**
 * The vanity import path all of the repository's Go modules live under. Every page, the 404 page included, carries
 * these meta tags, which is all `go get` needs: it reads only the <head>, even of a 404, so any path under the prefix
 * resolves to the repository. postbuild.ts fails the build if a page is missing them.
 */
export const GO_IMPORT_PATH = "getsteep.dev/steep";
export const GO_IMPORT = `${GO_IMPORT_PATH} git ${REPO_URL}`;
export const GO_SOURCE = `${GO_IMPORT_PATH} ${REPO_URL} ${REPO_URL}/tree/master{/dir} ${REPO_URL}/blob/master{/dir}/{file}#L{line}`;
