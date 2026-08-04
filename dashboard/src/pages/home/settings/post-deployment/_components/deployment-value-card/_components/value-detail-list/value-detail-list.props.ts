/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ValueReading } from "../../../../values.types";

export interface ValueDetailListProps {
  reading: ValueReading;
  /** i18n key for the "how to raise this" guidance. */
  noteKey: string;
}
