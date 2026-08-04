/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CopiableText } from "@espressif/dashboard-ui-components/components";
import type { ReactNode } from "react";
import { hasRenderableContent } from "../internal-page-header.utils";

interface InternalPageHeaderMetaStripProps {
  resourceLabel: string;
  resourceId: string;
  metaEnd: ReactNode;
}

export default function InternalPageHeaderMetaStrip({
  resourceLabel,
  resourceId,
  metaEnd,
}: InternalPageHeaderMetaStripProps) {
  const showResource = resourceLabel !== "" || resourceId !== "";

  return (
    <div className="flex flex-col gap-3 bg-secondary/5 px-5 py-2 sm:flex-row sm:items-center sm:justify-between sm:gap-2">
      {showResource ? (
        <p className="min-w-0 break-all text-xs text-muted-foreground">
          {resourceLabel !== "" ? (
            <>
              {resourceLabel}
              {resourceId !== "" ? ": " : null}
            </>
          ) : null}
          {resourceId !== "" ? (
            <CopiableText
              text={resourceId}
              className="text-xs text-muted-foreground"
            />
          ) : null}
        </p>
      ) : null}
      {hasRenderableContent(metaEnd) ? (
        <div className="shrink-0 sm:ml-auto flex items-center">{metaEnd}</div>
      ) : null}
    </div>
  );
}
