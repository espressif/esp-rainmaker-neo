/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

/** All interactive/display strings, already localised by the caller. */
export interface UrlListManagerLabels {
  /** Trigger + confirm button in the add popover, e.g. "Add URL". */
  addAction: string;
  /** Cancel button in the add popover. */
  cancelAction: string;
  /** Label for the URL input inside the popover. */
  inputLabel: string;
  /** Placeholder for the URL input. */
  inputPlaceholder: string;
  /** Shown in the card body when the list is empty. */
  emptyState: string;
  /** aria-label for each row's delete button. */
  deleteAriaLabel: string;
  /** Inline popover error when the input is empty. */
  requiredError: string;
  /** Inline popover error when the URL has already been added. */
  duplicateError: string;
}

export interface UrlListManagerProps {
  /** The current list of URLs. */
  value: string[];
  /** Called with the next list whenever a URL is added or removed. */
  onChange: (next: string[]) => void;
  /** Section card heading. */
  cardTitle: string;
  /** Optional section card sub-heading. */
  cardDescription?: string;
  /** Icon rendered in the card header and on each row. */
  icon?: ReactNode;
  /** Interactive/display strings (already localised by the caller). */
  labels: UrlListManagerLabels;
  /**
   * Optional list-level error (e.g. "add at least one"). Shown in place of the
   * empty state, or below the list when items are present.
   */
  error?: string;
  /**
   * When true the list is display-only: the add-URL popover and every row's
   * delete action are hidden. Useful for server-computed lists (e.g. GVA
   * redirect URIs) that are shown for reference but never edited.
   */
  readOnly?: boolean;
}
