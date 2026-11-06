"use client";

import { useDocsSearch } from "fumadocs-core/search/client";
import { flexsearchStaticClient } from "fumadocs-core/search/client/flexsearch-static";
import {
    SearchDialogClose,
    SearchDialogContent,
    SearchDialogFooter,
    SearchDialogHeader,
    SearchDialogIcon,
    SearchDialogInput,
    SearchDialogList,
    SearchDialogOverlay,
    SearchDialog as Dialog,
    TagsList,
    TagsListItem,
} from "fumadocs-ui/components/dialog/search";
import type { SharedProps } from "fumadocs-ui/contexts/search";
import { useState } from "react";

const ALL = "all";
const TAGS = [
    { value: ALL, label: "All" },
    { value: "steep", label: "Steep" },
    { value: "reel", label: "Reel" },
    { value: "cli", label: "Reel CLI" },
];

/** The site's search, with a filter for the section to look in. */
export function SearchDialog(props: SharedProps) {
    const [tag, setTag] = useState(ALL);
    const { search, setSearch, query } = useDocsSearch({
        client: flexsearchStaticClient({ from: "/api/search", tag: tag === ALL ? undefined : tag }),
    });
    return (
        <Dialog search={search} onSearchChange={setSearch} isLoading={query.isLoading} {...props}>
            <SearchDialogOverlay />
            <SearchDialogContent>
                <SearchDialogHeader>
                    <SearchDialogIcon />
                    <SearchDialogInput />
                    <SearchDialogClose />
                </SearchDialogHeader>
                <SearchDialogList items={query.data !== "empty" ? query.data : null} />
                <SearchDialogFooter>
                    <TagsList tag={tag} onTagChange={(v) => setTag(v ?? ALL)}>
                        {TAGS.map(({ value, label }) => (
                            <TagsListItem key={value} value={value}>
                                {label}
                            </TagsListItem>
                        ))}
                    </TagsList>
                </SearchDialogFooter>
            </SearchDialogContent>
        </Dialog>
    );
}
