/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ParsedQueryRules } from "../../query-rules-list.utils";

export interface QueryRulesListContentProps {
  /** Non-empty query string. The shell has already guarded the empty case. */
  queryString: string;
  /** Parsed rules, or `null` when the query has no flat-list representation. */
  parsedRules: ParsedQueryRules | null;
}
