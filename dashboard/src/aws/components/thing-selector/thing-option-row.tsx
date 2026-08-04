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
import { SelectorOptionRow } from "@/components/selector-option-row";

interface ThingOptionRowProps {
  label: string;
  secondaryText?: string;
}

/**
 * Node/thing option row: the shared selector row skeleton with the same device
 * avatar the Nodes page uses, kept static here (no live type/status) but with a
 * solid primary ring so it matches the group selector's rows.
 */
export function ThingOptionRow({ label, secondaryText }: ThingOptionRowProps) {
  return (
    <SelectorOptionRow
      label={label}
      secondaryText={secondaryText}
      avatar={
        <IconAvatar
          color="primary"
          size={32}
          ring={{ show: true, color: "primary" }}
        >
          <DeviceIcon type="" size={16} fallback={Microchip} />
        </IconAvatar>
      }
    />
  );
}
