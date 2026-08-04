/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface RegisterNodesFormProps {
  /** Pre-uploaded node certificate CSV handed off from the generate flow. */
  initialCertificateFile?: File;
  onSuccess?: () => void;
  onCancel?: () => void;
}
