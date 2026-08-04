/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@espressif/dashboard-ui-components/components";
import { useAuthStore } from "@/stores/auth.store";
import { getFieldValues } from "@/aws/services/thing.service";
import { advancedSearchFieldsData } from "@/aws/components/advanced-indices-search/things-indices-search-config";
import { AGGREGATABLE_RULE_FIELDS } from "../../query-rule-builder.constants";
import type { QueryRuleValueFieldProps } from "./query-rule-value-field.props";

const AGGREGATION_STALE_TIME = 5 * 60 * 1000;

/** Shared padding so the value control lines up with the Type `Select` (px-3). */
const CONTROL_CLASS = "px-3";

export default function QueryRuleValueField({
  fieldName,
  value,
  onChange,
  disabled,
}: QueryRuleValueFieldProps) {
  const { t } = useTranslation("common");
  const credentials = useAuthStore((s) => s.credentials);

  const fieldDef = advancedSearchFieldsData.find((f) => f.name === fieldName);
  const isBoolean = fieldDef?.type === "Boolean";
  const hasAggregation = AGGREGATABLE_RULE_FIELDS.has(fieldName);

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ["iot", "rule-field-values", fieldName],
    queryFn: () => getFieldValues(fieldName),
    enabled: !!credentials && !!fieldName && hasAggregation,
    staleTime: AGGREGATION_STALE_TIME,
  });

  const aggregationOptions = useMemo(
    () =>
      (data ?? [])
        // A Select item cannot have an empty value, so drop blank buckets.
        .filter((bucket) => bucket.value !== "")
        .map((bucket) => ({
          value: bucket.value,
          label: `${bucket.value} (${bucket.count})`,
        })),
    [data],
  );

  const selectPlaceholder = t(
    "queryRuleBuilder.valuePlaceholderSelect",
    "Select a value",
  );

  if (isBoolean) {
    return (
      <Select
        value={value || undefined}
        onValueChange={onChange}
        disabled={disabled}
      >
        <SelectTrigger size="sm" className={CONTROL_CLASS}>
          <SelectValue placeholder={selectPlaceholder} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="true">
            {t("queryRulesList.booleanTrue", "True")}
          </SelectItem>
          <SelectItem value="false">
            {t("queryRulesList.booleanFalse", "False")}
          </SelectItem>
        </SelectContent>
      </Select>
    );
  }

  if (hasAggregation) {
    const isBusy = isLoading || isFetching;
    return (
      <Select
        value={value || undefined}
        onValueChange={onChange}
        disabled={disabled || isBusy}
      >
        <SelectTrigger size="sm" className={CONTROL_CLASS}>
          <SelectValue
            placeholder={
              isBusy
                ? t("queryRuleBuilder.valueLoading", "Loading values…")
                : selectPlaceholder
            }
          />
        </SelectTrigger>
        <SelectContent>
          {aggregationOptions.map((option) => (
            <SelectItem key={option.value} value={option.value}>
              {option.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    );
  }

  return (
    <Input
      type="text"
      size="sm"
      className={CONTROL_CLASS}
      value={value}
      disabled={disabled}
      onChange={(event) => onChange(event.target.value)}
      placeholder={t("queryRuleBuilder.valuePlaceholderText", "Enter a value")}
    />
  );
}
