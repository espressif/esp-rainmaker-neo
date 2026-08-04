/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Cpu } from "lucide-react";
import {
  DeviceIcon,
  IconAvatar,
} from "@espressif/dashboard-ui-components/components";
import type { FirmwareAvatarProps } from "./firmware-avatar.props";

const DEFAULT_SIZE = 40;

/**
 * Avatar for an OTA firmware image. Before the tagging call resolves (`fwType`
 * undefined) it shows the `Cpu` glyph — the same icon used for the "Images"
 * sidebar item. Once `fwType` is known, `DeviceIcon` resolves a matching glyph
 * and falls back to `Cpu` for unknown types.
 */
export function FirmwareAvatar({ fwType, size = DEFAULT_SIZE }: FirmwareAvatarProps) {
  return (
    <IconAvatar size={size} color="primary" ring={{ show: true, color: "primary" }}>
      <DeviceIcon
        type={fwType ?? ""}
        size={Math.round(size / 2)}
        fallback={Cpu}
      />
    </IconAvatar>
  );
}
