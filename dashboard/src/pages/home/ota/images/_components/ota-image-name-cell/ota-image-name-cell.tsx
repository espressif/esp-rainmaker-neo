/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CopiableText, Badge } from "@espressif/dashboard-ui-components/components";
import { FirmwareAvatar } from "@/components/avatars/firmware-avatar";
import type { OtaImageNameCellProps } from "./ota-image-name-cell.props";

function formatSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

export function OtaImageNameCell({ name, size, md5, fwType }: OtaImageNameCellProps) {
  return (
    <div className="flex min-w-0 items-center gap-3">
      <FirmwareAvatar fwType={fwType} />
      <div className="min-w-0 flex flex-col">
        <div className="flex items-center gap-2">
          <p className="text-sm font-semibold truncate leading-tight">{name}</p>
          <Badge
            variant="soft"
            className="font-medium !border border-solid p-0.5"
            color="error"
          >
            {formatSize(size)}
          </Badge>
        </div>
        {md5 ? (
          <CopiableText
            text={md5}
            className="text-xs font-mono text-muted-foreground truncate leading-tight"
          />
        ) : null}
      </div>
    </div>
  );
}
