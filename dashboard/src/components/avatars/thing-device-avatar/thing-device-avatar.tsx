/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Microchip } from "lucide-react";
import {
  DeviceIcon,
  IconAvatar,
} from "@espressif/dashboard-ui-components/components";
import {
  getThingDisplayStatus,
  getThingStatusPresentation,
} from "@/config/node-status.config";
import type { ThingDeviceAvatarProps } from "./thing-device-avatar.props";

const DEFAULT_SIZE = 40;

export function ThingDeviceAvatar({
  deviceType,
  online,
  size = DEFAULT_SIZE,
}: ThingDeviceAvatarProps) {
  const status = getThingDisplayStatus(online);
  const { color } = getThingStatusPresentation(status);

  return (
    <IconAvatar
      size={size}
      color={color}
      ring={{
        show: status != null,
        pulsate: status === "online",
        color,
      }}
    >
      <DeviceIcon
        type={deviceType ?? ""}
        size={Math.round(size / 2)}
        fallback={Microchip}
      />
    </IconAvatar>
  );
}
