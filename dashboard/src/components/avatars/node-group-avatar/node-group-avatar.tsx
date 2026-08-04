/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Group } from "lucide-react";
import { IconAvatar } from "@espressif/dashboard-ui-components/components";
import type { NodeGroupAvatarProps } from "./node-group-avatar.props";

const DEFAULT_SIZE = 36;
const COLOR = "info" as const;

export function NodeGroupAvatar({ size = DEFAULT_SIZE }: NodeGroupAvatarProps) {
  const iconSize = Math.round(size / 2);

  return (
    <IconAvatar
      size={size}
      color={COLOR}
      ring={{ show: true, color: COLOR }}
      className="shrink-0"
    >
      <Group size={iconSize} className="shrink-0" aria-hidden />
    </IconAvatar>
  );
}
