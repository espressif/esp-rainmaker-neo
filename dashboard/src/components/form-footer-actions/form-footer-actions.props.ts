/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

/** A single button in the footer action row. */
export interface FormFooterAction {
  label: string;
  onClick?: () => void;
  startIcon?: ReactNode;
  loading?: boolean;
  disabled?: boolean;
  /** Defaults to "button". Use "submit" to trigger the enclosing form. */
  type?: "button" | "submit";
}

export interface FormFooterActionsProps {
  /**
   * Left-aligned slot for a destructive action (e.g. a delete trigger that
   * opens a confirmation dialog). Rendered as-is so callers keep full control.
   */
  destructiveAction?: ReactNode;
  /** Secondary right-aligned action, e.g. Cancel. Omit to show only the primary. */
  softAction?: FormFooterAction;
  /** Primary right-aligned action, e.g. the form submit. */
  primaryAction: FormFooterAction;
  className?: string;
}
