/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  MonospaceContent,
  SimpleList,
} from "@espressif/dashboard-ui-components/components";
import type { QueryRulesListContentProps } from "./query-rules-list-content.props";

/**
 * Renders the parsed rules, or the raw query string when the query cannot be
 * represented as a flat rule list (nested expressions, mixed AND/OR).
 */
export default function QueryRulesListContent({
  queryString,
  parsedRules,
}: QueryRulesListContentProps) {
  if (!parsedRules) {
    return (
      <MonospaceContent value={queryString} color="gray" className="text-xs" />
    );
  }

  return (
    <SimpleList
      items={parsedRules.items}
      size="sm"
      direction="column"
      iconStyle="inline"
      separators={true}
    />
  );
}
