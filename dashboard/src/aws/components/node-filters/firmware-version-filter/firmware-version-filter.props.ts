/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type FirmwareVersionFilterValue = string;

export interface FirmwareVersionFilterProps {
  value: FirmwareVersionFilterValue | null;
  onChange: (value: FirmwareVersionFilterValue | null) => void;
  disabled?: boolean;
}
