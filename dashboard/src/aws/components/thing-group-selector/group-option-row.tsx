/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Group } from "lucide-react";
import { IconAvatar } from "@espressif/dashboard-ui-components/components";
import { SelectorOptionRow } from "@/components/selector-option-row";

interface GroupOptionRowProps {
  label: string;
  secondaryText?: string;
}

/**
 * Group option row: the shared selector row skeleton with a primary-ringed
 * avatar carrying the `Group` glyph.
 */
export function GroupOptionRow({ label, secondaryText }: GroupOptionRowProps) {
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
          <Group className="h-4 w-4" aria-hidden />
        </IconAvatar>
      }
    />
  );
}
