/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  Badge,
} from "@espressif/dashboard-ui-components/components";

interface ValueSuggestionsProps {
  suggestions: { value: string; count: number }[];
  onSelect: (value: string) => void;
}

export function ValueSuggestions({
  suggestions,
  onSelect,
}: ValueSuggestionsProps) {
  const { t } = useTranslation("nodes");

  return (
    <DropdownMenuGroup>
      <DropdownMenuLabel className="text-xs text-muted-foreground">
        {t(
          "advancedIndicesSearch.searchBarContent.suggestedValues",
          "Suggested values",
        )}
      </DropdownMenuLabel>
      {suggestions.map((s) => (
        <DropdownMenuItem
          key={s.value}
          className="justify-between"
          onSelect={(e) => {
            e.preventDefault();
            onSelect(s.value);
          }}
        >
          <span className="truncate text-sm">{s.value}</span>
          <Badge
            variant="outline"
            className="shrink-0 text-[10px] font-normal text-muted-foreground rounded-md px-1.5"
          >
            {s.count}
          </Badge>
        </DropdownMenuItem>
      ))}
    </DropdownMenuGroup>
  );
}
