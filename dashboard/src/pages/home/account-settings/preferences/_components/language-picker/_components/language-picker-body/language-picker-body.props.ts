/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SelectableCardListItem } from "@espressif/dashboard-ui-components/components";

export interface LanguagePickerBodyProps {
  /** Languages left after the search filter; empty renders the no-results state. */
  items: SelectableCardListItem[];
  /** Code of the language currently in use. */
  value: string;
  /** Receives the selected row id; the caller validates it before persisting. */
  onChange: (code: string) => void;
  ariaLabel: string;
}
