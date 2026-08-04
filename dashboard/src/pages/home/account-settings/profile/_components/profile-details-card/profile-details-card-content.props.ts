/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  DynamicListEntry,
  DynamicListMetaEntry,
} from "@espressif/dashboard-ui-components/components";

export interface ProfileDetailsCardContentProps {
  /** Rows built from the id_token claims; empty when the token is unreadable. */
  items: DynamicListEntry[];
  meta: Record<string, DynamicListMetaEntry>;
}
