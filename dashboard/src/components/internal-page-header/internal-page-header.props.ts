/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

export interface InternalPageHeaderProps {
  /** When set with {@link backLinkHref}, label for the back link. If omitted but href is set, the label defaults to "Back". */
  backLinkLabel?: string;
  /** When set (non-empty after trim), shows a back {@link Link} above the header. No link when absent or blank. */
  backLinkHref?: string;
  /** Shown in the top strip with {@link resourceId} (e.g. "User ID"). Omit the strip when both are absent. */
  resourceLabel?: string;
  /** Copiable identifier in the top strip (e.g. UUID). Omit the strip when both label and id are absent. */
  resourceId?: string;
  /** Right-aligned content in the top metadata strip, such as a date or status badge. */
  metaEnd?: ReactNode;
  /** Final rendered avatar/visual shown beside the heading. */
  avatar?: ReactNode;
  /** Primary title. Omit when only a back link (or meta strip) is needed. */
  heading?: ReactNode;
  /** Secondary line (e.g. last activity). Omit to hide. */
  description?: ReactNode;
  /** Right-side content in the title row, such as buttons, menus, badges, or cards. */
  actions?: ReactNode;
  /** Content rendered below the title row, such as tabs. */
  footer?: ReactNode;
  /** Optional className for the outer header container. */
  className?: string;
}
