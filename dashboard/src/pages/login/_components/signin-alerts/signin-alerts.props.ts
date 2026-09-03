/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LoginSearch } from "../../_schema/login-search.schema";

export interface SigninAlertsProps {
  /** Already-translated failure text; `null` renders no error alert. */
  errorMessage: string | null;
  /**
   * `?reset` outcome from the query string. Only the entry screens pass it —
   * the success banner should not follow the admin into the flow.
   */
  resetOutcome?: LoginSearch["reset"];
}
