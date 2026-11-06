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
