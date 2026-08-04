/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  DropdownMenuGroup,
  DropdownMenuItem,
} from "@espressif/dashboard-ui-components/components";

interface ValueSelectorProps {
  onSelect: (val: "true" | "false") => void;
}

export function ValueSelector({ onSelect }: ValueSelectorProps) {
  return (
    <DropdownMenuGroup>
      {(["true", "false"] as const).map((val) => (
        <DropdownMenuItem
          key={val}
          onSelect={(e) => {
            e.preventDefault();
            onSelect(val);
          }}
        >
          <span className="text-sm text-foreground">{val}</span>
        </DropdownMenuItem>
      ))}
    </DropdownMenuGroup>
  );
}
