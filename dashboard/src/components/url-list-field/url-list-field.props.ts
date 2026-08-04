/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react";
import type { UrlListManagerLabels } from "@/components/url-list-manager";

/** Re-exported so form consumers can type their labels from one import. */
export type UrlListFieldLabels = UrlListManagerLabels;

export interface UrlListFieldProps {
  /**
   * react-hook-form field-array path. The bound field must be an array of
   * `{ value: string }` objects and live inside the surrounding `<Form>`
   * (FormProvider) context. Validation (min length, format) belongs to the
   * consumer's schema; the underlying manager only enforces non-empty +
   * no-duplicates inside the add popover.
   */
  name: string;
  /** Section card heading. */
  cardTitle: string;
  /** Optional section card sub-heading. */
  cardDescription?: string;
  /** Icon rendered in the card header and on each row. */
  icon?: ReactNode;
  /** Interactive/display strings (already localised by the caller). */
  labels: UrlListFieldLabels;
}
