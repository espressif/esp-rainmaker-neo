/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface CreateOtaJobFormProps {
  /** S3 key of a firmware image to pre-select (e.g. from a `?firmware_key=` deep link). */
  firmwareKey?: string;
  onSuccess?: () => void;
  onCancel?: () => void;
}
