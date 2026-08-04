/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import { cn } from "@/utils/utils";
import type { InternalPageHeaderVisibility } from "../internal-page-header.utils";

interface InternalPageHeaderTitleRowProps {
  visibility: Pick<
    InternalPageHeaderVisibility,
    "showAvatar" | "showHeading" | "showDescription" | "showActions"
  >;
  avatar: ReactNode;
  heading: ReactNode;
  description: ReactNode;
  actions: ReactNode;
}

export default function InternalPageHeaderTitleRow({
  visibility,
  avatar,
  heading,
  description,
  actions,
}: InternalPageHeaderTitleRowProps) {
  return (
    <div className="flex flex-col gap-6 px-5 pt-3 lg:flex-row lg:items-stretch lg:justify-between lg:gap-8">
      <div
        className={cn(
          "min-w-0",
          visibility.showAvatar
            ? "flex flex-1 items-stretch gap-4"
            : "flex flex-1 flex-col gap-1",
        )}
      >
        {visibility.showAvatar ? (
          <div className="shrink-0 self-start">{avatar}</div>
        ) : null}
        {/*
          `justify-center` centers the text against a taller avatar (heading only);
          once the text outgrows the avatar the row grows instead and the
          `self-start` avatar stays pinned to the top.
        */}
        <div className="flex min-w-0 flex-1 flex-col justify-center">
          {visibility.showHeading ? (
            <div className="break-words text-2xl font-bold tracking-tight text-foreground">
              {heading}
            </div>
          ) : null}
          {visibility.showDescription ? (
            // `empty:hidden` drops the spacing when the slot's component renders nothing.
            <div className="mt-1 text-sm text-muted-foreground empty:hidden">
              {description}
            </div>
          ) : null}
        </div>
      </div>

      {visibility.showActions ? (
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-3 self-stretch lg:pt-1">
          {actions}
        </div>
      ) : null}
    </div>
  );
}
