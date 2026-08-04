/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";

export interface SmsSandboxCardProps {
  /** Section-card icon, so this card matches the read-only value cards beside it. */
  Icon: LucideIcon;
  /** i18n key of the card title, so the copy stays alongside the other post-deployment values. */
  titleKey: string;
  /** i18n key of the production note explaining why exiting the SMS sandbox matters. */
  noteKey: string;
}
