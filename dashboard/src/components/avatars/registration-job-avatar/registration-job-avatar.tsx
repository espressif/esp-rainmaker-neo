/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { FileText } from "lucide-react";
import { IconAvatar } from "@espressif/dashboard-ui-components/components";
import type { RegistrationJobAvatarProps } from "./registration-job-avatar.props";

const DEFAULT_SIZE = 36;

export function RegistrationJobAvatar({
  size = DEFAULT_SIZE,
  failedCount,
}: RegistrationJobAvatarProps) {
  const color = failedCount === 0 ? "success" : "error";
  const iconSize = Math.round(size / 2);

  return (
    <IconAvatar
      size={size}
      color={color}
      ring={{ show: true, color }}
      className="shrink-0"
    >
      <FileText size={iconSize} className="shrink-0" aria-hidden />
    </IconAvatar>
  );
}
