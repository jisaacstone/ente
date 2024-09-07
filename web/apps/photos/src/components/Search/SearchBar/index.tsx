import { Collection } from "types/collection";
import { SearchBarMobile } from "./searchBarMobile";

import { UpdateSearch } from "@/new/photos/services/search/types";
import { EnteFile } from "@/new/photos/types/file";
import SearchInput from "./searchInput";
import { SearchBarWrapper } from "./styledComponents";

interface Props {
    updateSearch: UpdateSearch;
    collections: Collection[];
    files: EnteFile[];
    isInSearchMode: boolean;
    setIsInSearchMode: (v: boolean) => void;
}

export default function SearchBar({
    setIsInSearchMode,
    isInSearchMode,
    ...props
}: Props) {
    const showSearchInput = () => setIsInSearchMode(true);

    return (
        <SearchBarWrapper>
            <SearchInput
                {...props}
                isOpen={isInSearchMode}
                setIsOpen={setIsInSearchMode}
            />
            <SearchBarMobile
                show={!isInSearchMode}
                showSearchInput={showSearchInput}
            />
        </SearchBarWrapper>
    );
}
