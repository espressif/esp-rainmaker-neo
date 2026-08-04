/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";

export interface OnboardingCardProps {
  /** Lucide icon rendered in the card header, sized `w-6 h-6` by the caller. */
  icon: ReactNode;
  /** Card heading, e.g. "Sign in" or "Reset password". */
  title: ReactNode;
  /**
   * Sub-heading. Rendered in the card body rather than `SectionCard`'s
   * `secondaryText`, which forces the text onto a single truncated line.
   */
  description?: ReactNode;
  /** Card body — typically a form. */
  children: ReactNode;
  /** Header slot opposite the title, e.g. a "Back to sign in" link. */
  actions?: ReactNode;
  className?: string;
}
