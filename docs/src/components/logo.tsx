import dark from "../assets/wordmark-dark-animated.svg";
import darkSmall from "../assets/wordmark-dark-animated-small.svg";
import light from "../assets/wordmark-light-animated.svg";
import lightSmall from "../assets/wordmark-light-animated-small.svg";

/**
 * The teacup and pixel wordmark with its steam rising, in the palette for the page's theme. The light palette's rows
 * are deeper. By default it's the navbar's: small, with bolder steam, and nudged up, since the steam leaves the cup
 * and letters low in the image.
 */
export function Logo({ large = false }: { large?: boolean }) {
    const className = large ? "" : "h-7 -translate-y-[5px]";
    return (
        <>
            <img src={large ? light : lightSmall} alt="steep" className={`w-auto dark:hidden ${className}`} />
            <img src={large ? dark : darkSmall} alt="steep" className={`hidden w-auto dark:block ${className}`} />
        </>
    );
}
