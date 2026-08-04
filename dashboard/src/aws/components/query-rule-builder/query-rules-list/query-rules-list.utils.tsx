/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import {
  Typography,
  type SimpleListItem,
} from "@espressif/dashboard-ui-components/components";
import type {
  Junction,
  SearchCondition,
} from "@/aws/components/advanced-indices-search";
// Imported from the modules directly rather than the barrel: those are pure
// functions, while the barrel also pulls in the search-bar UI at runtime.
import { FIELD_TYPE_ICONS } from "@/aws/components/advanced-indices-search/field-type-icons";
import { OPERATORS_BY_TYPE } from "@/aws/components/advanced-indices-search/operator-config";
import { parseQueryString } from "@/aws/components/advanced-indices-search/query-parser";
import { advancedSearchFieldsData } from "@/aws/components/advanced-indices-search/things-indices-search-config";
import { fieldLabel } from "@/aws/components/advanced-indices-search/field-label";
import { ONLINE_STATUS_FIELD } from "./query-rules-list.constants";

export type QueryRulesCombinator = Junction;

export interface ParsedQueryRules {
  items: SimpleListItem[];
  /** How the rules combine. Drives the caption above the list. */
  combinator: QueryRulesCombinator;
}

/**
 * Parses a fleet-index query string into `SimpleList` rows.
 *
 * Returns `null` when the query cannot be faithfully represented as a flat rule
 * list — nothing parsed at all, or the rules mix `AND` and `OR` junctions, which
 * a flat list would misrepresent. Callers show the raw query string instead.
 */
export function buildQueryRuleItems(
  queryString: string,
  t: TFunction,
): ParsedQueryRules | null {
  const { conditions, junctions } = parseQueryString(
    queryString,
    advancedSearchFieldsData,
  );

  if (conditions.length === 0) {
    return null;
  }

  const uniqueJunctions = new Set(junctions);
  if (uniqueJunctions.size > 1) {
    return null;
  }

  const items = conditions.map((condition, index) => {
    const definition = findFieldDefinition(condition.field);
    return {
      key: `${condition.field}-${condition.operator}-${index}`,
      label: definition ? fieldLabel(definition, t) : condition.field,
      icon: definition?.icon ?? FIELD_TYPE_ICONS[condition.fieldType],
      content: (
        <Typography variant="body2">
          {formatConditionValue(condition, t)}
        </Typography>
      ),
    };
  });

  return { items, combinator: junctions[0] ?? "AND" };
}

/**
 * Caption describing how the rules combine. `undefined` for an unparsed query,
 * where the raw string is shown and there is no combinator to describe.
 */
export function resolveCombinatorCaption(
  parsedRules: ParsedQueryRules | null,
  t: TFunction,
): string | undefined {
  if (!parsedRules) {
    return undefined;
  }
  if (parsedRules.combinator === "OR") {
    return t("queryRulesList.matchAny", "Any of the following must match");
  }
  return t("queryRulesList.matchAll", "All of the following must match");
}

/**
 * `undefined` for custom tag fields, which are typed into the search bar and so
 * never appear in the catalog.
 */
function findFieldDefinition(field: string) {
  return advancedSearchFieldsData.find(
    (definition) => definition.name === field,
  );
}

function formatConditionValue(
  condition: SearchCondition,
  t: TFunction,
): string {
  if (condition.operator === "exists") {
    return t("queryRulesList.operators.exists", "Any value");
  }

  const value = formatValue(condition, t);

  if (condition.operator === "eq") {
    return value;
  }

  return `${resolveOperatorLabel(condition, t)} ${value}`;
}

/**
 * Operator labels in `OPERATORS_BY_TYPE` are untranslated (they back the search
 * bar's symbol chips), so they serve as the English fallback while the rendered
 * label stays translatable.
 */
function resolveOperatorLabel(
  condition: SearchCondition,
  t: TFunction,
): string {
  const fallback =
    OPERATORS_BY_TYPE[condition.fieldType].find(
      (operator) => operator.id === condition.operator,
    )?.label ?? condition.operator;

  return t(`queryRulesList.operators.${condition.operator}`, fallback);
}

function formatValue(condition: SearchCondition, t: TFunction): string {
  if (condition.fieldType !== "Boolean") {
    return condition.value;
  }

  const isTrue = condition.value === "true";

  if (condition.field !== ONLINE_STATUS_FIELD) {
    if (isTrue) {
      return t("queryRulesList.booleanTrue", "True");
    }
    return t("queryRulesList.booleanFalse", "False");
  }

  if (isTrue) {
    return t("queryRulesList.online", "Online");
  }
  return t("queryRulesList.offline", "Offline");
}
