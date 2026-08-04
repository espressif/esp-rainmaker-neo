/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export type FieldType = "Number" | "String" | "Boolean";

export type SupportedIndexName = "AWS_Things" | "AWS_ThingGroups";

/**
 * The part of a field definition the query parser needs. Kept separate from
 * `IndexField` so callers with their own field catalog (a different `icon`
 * shape, extra metadata) can parse without conforming to the dropdown's props.
 */
export interface IndexFieldRef {
  /** The field path used in the query (e.g. "thingName", "shadow.name.iparams.reported.online") */
  name: string;
  type: FieldType;
  /** Optional user-friendly display label. Falls back to name if not provided. */
  label?: string;
  /** Fully-qualified i18n key for `label`, which serves as its English fallback. */
  labelKey?: string;
}

export interface IndexField extends IndexFieldRef {
  /** Optional icon element rendered beside the field in the dropdown. */
  icon?: React.ReactNode;
}

export type OperatorId =
  | "eq"
  | "neq"
  | "starts_with"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "exists";

export interface Operator {
  id: OperatorId;
  /** Badge token. A symbol for most operators, hence translated only when `labelKey` is set. */
  label: string;
  /** i18n key under `common` for `label`; absent when the label is a symbol. */
  labelKey?: string;
  /** English fallback for `descriptionKey`. */
  description: string;
  /** i18n key under `common` for `description`. */
  descriptionKey: string;
  /** If true, no value input is needed (e.g. "exists") */
  noValue?: boolean;
}

export type Junction = "AND" | "OR";

export interface SearchCondition {
  field: string;
  fieldType: FieldType;
  operator: OperatorId;
  value: string;
}

export type InputStep = "idle" | "field" | "operator" | "value";

export interface AdvancedIndicesSearchProps {
  /** Searchable fields with their query paths, types, and display labels. */
  fields: IndexField[];
  query?: string;
  maxAllowedConditions?: number;
  /** Index name for GetBucketsAggregation value suggestions. Defaults to "AWS_Things". */
  indexName?: SupportedIndexName;
  onSearch: (query: string) => void;
  children: React.ReactNode;
}
