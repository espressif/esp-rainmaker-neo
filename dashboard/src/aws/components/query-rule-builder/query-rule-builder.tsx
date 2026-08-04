/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import { ListFilter } from "lucide-react";
import type { TFunction } from "i18next";
import { advancedSearchFieldsData } from "@/aws/components/advanced-indices-search/things-indices-search-config";
import { fieldLabel } from "@/aws/components/advanced-indices-search/field-label";
import type { QueryRule, QueryRuleBuilderProps } from "./query-rule-builder.props";
import { QueryRulePopover } from "./_components/query-rule-popover";
import {
  QueryRulesContent,
  type QueryRuleRow,
} from "./_components/query-rules-content";

/** Falls back to the raw field path for fields outside the catalog. */
function resolveRuleFieldLabel(field: string, t: TFunction): string {
  const definition = advancedSearchFieldsData.find((f) => f.name === field);
  return definition ? fieldLabel(definition, t) : field;
}

/**
 * A controlled builder for AWS IoT fleet-index query rules. Renders a card with
 * an "Add rule" popover and a table of the current rules; add/remove flow back
 * out through `onChange`, so the parent owns storage (a form field array, local
 * state, …). Field options come from the shared `advancedSearchFieldsData`
 * catalog. Serialise with `buildQueryFromRules`.
 */
export function QueryRuleBuilder({
  rules,
  onChange,
  title,
  description,
}: QueryRuleBuilderProps) {
  const { t } = useTranslation("common");

  const rows = useMemo<QueryRuleRow[]>(
    () =>
      rules.map((rule, index) => ({
        id: `${rule.field}:${rule.value}:${index}`,
        type: resolveRuleFieldLabel(rule.field, t),
        value: rule.value,
      })),
    [rules, t],
  );

  const handleAdd = (rule: QueryRule) => onChange([...rules, rule]);

  const handleDelete = (index: number) =>
    onChange(rules.filter((_, position) => position !== index));

  return (
    <SectionCard
      icon={<ListFilter className="h-5 w-5" aria-hidden />}
      primaryText={title ?? t("queryRuleBuilder.label", "Rules")}
      secondaryText={
        description ??
        t(
          "queryRuleBuilder.description",
          "Add rules to dynamically match the nodes this targets.",
        )
      }
      allowCollapse={false}
      size="sm"
      variant="outline"
      color="mist"
      actions={<QueryRulePopover onAdd={handleAdd} />}
    >
      <QueryRulesContent rules={rows} onDelete={handleDelete} />
    </SectionCard>
  );
}
