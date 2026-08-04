/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { GvaConfigGetResponse } from "@/api/integrations";

export interface GvaConfigFormSheetProps {
  /** Saved configuration used to seed the form; absent when configuring anew. */
  initialData?: GvaConfigGetResponse;
  /** Closes the sheet (mapped from the form's cancel/success callbacks). */
  onClose: () => void;
}
