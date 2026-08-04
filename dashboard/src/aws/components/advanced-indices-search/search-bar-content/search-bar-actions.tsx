/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Search, X } from "lucide-react";
import { Button } from "@espressif/dashboard-ui-components/components";

interface SearchBarActionsProps {
  disabled: boolean;
  onClear: () => void;
  onSearch: () => void;
}

export function SearchBarActions({
  disabled,
  onClear,
  onSearch,
}: SearchBarActionsProps) {
  return (
    <div className="ml-auto flex items-center gap-2 shrink-0">
      <Button
        variant="outline"
        size="icon"
        fullWidth={false}
        className="rounded-full text-destructive hover:text-destructive"
        disabled={disabled}
        onClick={(e) => {
          e.stopPropagation();
          onClear();
        }}
      >
        <X className="h-4 w-4" />
      </Button>
      <Button
        variant="outline"
        size="icon"
        fullWidth={false}
        className="rounded-full"
        disabled={disabled}
        onClick={(e) => {
          e.stopPropagation();
          onSearch();
        }}
      >
        <Search className="h-4 w-4" />
      </Button>
    </div>
  );
}
