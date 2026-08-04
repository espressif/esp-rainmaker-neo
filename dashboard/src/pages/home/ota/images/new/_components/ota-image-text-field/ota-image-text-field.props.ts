/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** Names of the text (string) fields on the upload form. */
export type OtaImageTextFieldName =
  | "name"
  | "version"
  | "type"
  | "model"
  | "platform";

export interface OtaImageTextFieldProps {
  name: OtaImageTextFieldName;
  label: string;
  placeholder?: string;
  required?: boolean;
}
