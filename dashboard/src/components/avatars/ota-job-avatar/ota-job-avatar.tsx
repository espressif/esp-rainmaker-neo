/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { CloudCog } from "lucide-react";
import { IconAvatar } from "@espressif/dashboard-ui-components/components";
import { getOtaJobStatusPresentation } from "@/config/ota-job-status.config";
import type { OtaJobAvatarProps } from "./ota-job-avatar.props";

const DEFAULT_SIZE = 36;

export function OtaJobAvatar({ size = DEFAULT_SIZE, status }: OtaJobAvatarProps) {
  const { color } = getOtaJobStatusPresentation(status);
  const iconSize = Math.round(size / 2);

  return (
    <IconAvatar
      size={size}
      color={color}
      ring={{ show: true, color }}
      className="shrink-0"
    >
      <CloudCog size={iconSize} className="shrink-0" aria-hidden />
    </IconAvatar>
  );
}
