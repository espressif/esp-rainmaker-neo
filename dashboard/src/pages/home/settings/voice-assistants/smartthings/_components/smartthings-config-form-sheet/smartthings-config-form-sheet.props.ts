/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SmartThingsConfigGetResponse } from "@/api/integrations";

export interface SmartThingsConfigFormSheetProps {
  /** Saved configuration used to seed the form; absent when configuring anew. */
  initialData?: SmartThingsConfigGetResponse;
  /** Closes the sheet (mapped from the form's cancel/success callbacks). */
  onClose: () => void;
}
