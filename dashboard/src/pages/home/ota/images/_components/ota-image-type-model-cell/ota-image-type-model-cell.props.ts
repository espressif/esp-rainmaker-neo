/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface OtaImageTypeModelCellProps {
  /** `fw-type` tag — rendered as the primary line. */
  type?: string;
  /** `fw-model` tag — rendered as the muted secondary line. */
  model?: string;
}
