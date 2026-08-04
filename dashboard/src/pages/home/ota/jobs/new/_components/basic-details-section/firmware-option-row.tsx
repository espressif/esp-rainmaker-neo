/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { FirmwareAvatar } from "@/components/avatars/firmware-avatar";
import { SelectorOptionRow } from "@/components/selector-option-row";

interface FirmwareOptionRowProps {
  name: string;
  size?: number;
  lastModified?: Date;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function buildSecondary(size?: number, lastModified?: Date): string | undefined {
  const parts: string[] = [];
  if (typeof size === "number") {
    parts.push(formatSize(size));
  }
  if (lastModified) {
    parts.push(lastModified.toLocaleDateString());
  }
  return parts.length > 0 ? parts.join(" · ") : undefined;
}

/**
 * Firmware-image option row for the generic `S3ListObjectsSelector` used on the
 * create-OTA-job form. The firmware-specific presentation lives here (in the
 * consumer), keeping the S3 selector resource-agnostic. Raw S3 listings carry
 * no fw-type tag, so `FirmwareAvatar` shows its default glyph.
 */
export function FirmwareOptionRow({
  name,
  size,
  lastModified,
}: FirmwareOptionRowProps) {
  return (
    <SelectorOptionRow
      label={name}
      secondaryText={buildSecondary(size, lastModified)}
      avatar={<FirmwareAvatar size={32} />}
    />
  );
}
