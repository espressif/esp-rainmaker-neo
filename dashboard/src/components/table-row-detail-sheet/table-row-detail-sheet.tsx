/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@espressif/dashboard-ui-components/components";
import { cn } from "@/utils/utils";
import type { TableRowDetailSheetProps } from "./table-row-detail-sheet.props";

export function TableRowDetailSheet({
  label,
  contentClassName,
  onOpenChange,
  children,
}: TableRowDetailSheetProps) {
  const [open, setOpen] = useState(true);

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        onOpenChange(next);
      }}
      closeOnEscape={false}
      closeOnOutsideClick={false}
      floating
    >
      <SheetContent
        side="right"
        className={cn("w-screen max-w-screen-lg", contentClassName)}
      >
        {label != null && label !== "" && (
          <SheetHeader className="items-start text-left">
            <SheetTitle>{label}</SheetTitle>
          </SheetHeader>
        )}
        <div className="p-5">{children}</div>
      </SheetContent>
    </Sheet>
  );
}
