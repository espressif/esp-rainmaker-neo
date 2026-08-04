/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { Badge, Button } from "@espressif/dashboard-ui-components/components";
import type {
  IndexField,
  Junction,
  SearchCondition,
} from "../advanced-indices-search.types";
import { fieldLabel } from "../field-label";
import { OPERATORS_BY_TYPE } from "../operator-config";

/** Falls back to the raw field path for custom tag fields, which are not in the catalog. */
function resolveFieldLabel(
  fields: IndexField[],
  condition: SearchCondition,
  t: TFunction,
): string {
  const field = fields.find((f) => f.name === condition.field);
  return field ? fieldLabel(field, t) : condition.field;
}

/** Word-shaped operators carry a `labelKey`; symbols (`=`, `>=`, …) render as-is. */
function resolveOperatorLabel(condition: SearchCondition, t: TFunction): string {
  const operator = OPERATORS_BY_TYPE[condition.fieldType]?.find(
    (op) => op.id === condition.operator,
  );
  if (!operator) {return condition.operator;}
  return operator.labelKey ? t(operator.labelKey, operator.label) : operator.label;
}

interface ConditionChipsProps {
  conditions: SearchCondition[];
  junctions: Junction[];
  fields: IndexField[];
  onToggleJunction: (index: number) => void;
  onRemoveCondition: (index: number) => void;
}

export function ConditionChips({
  conditions,
  junctions,
  fields,
  onToggleJunction,
  onRemoveCondition,
}: ConditionChipsProps) {
  const { t } = useTranslation("common");

  return (
    <>
      {conditions.map((condition, idx) => (
        <div key={`condition-${idx}`} className="flex items-center gap-1">
          {idx > 0 && (
            <Button
              variant="link"
              size="sm"
              fullWidth={false}
              hideRingOnHover
              onClick={(e) => {
                e.stopPropagation();
                onToggleJunction(idx - 1);
              }}
            >
              {junctions[idx - 1] ?? "AND"}
            </Button>
          )}

          <Badge variant="outline" className="rounded-full bg-accent border-primary/10 px-1">
            <span className="pl-2 py-1 font-medium text-muted-foreground max-w-40 truncate">
              {resolveFieldLabel(fields, condition, t)}
            </span>
            <span className="px-1 py-1 font-bold text-md text-muted-foreground">
              {resolveOperatorLabel(condition, t)}
            </span>
            {condition.value && (
              <span className="pr-1 py-1 font-medium truncate max-w-32">
                {condition.value}
              </span>
            )}
            <Button
              variant="ghost"
              size="icon"
              fullWidth={false}
              hideRingOnHover
              className="h-auto w-auto px-1 py-1 rounded-full hover:bg-destructive/10 hover:text-destructive"
              onClick={(e) => {
                e.stopPropagation();
                onRemoveCondition(idx);
              }}
            >
              <X className="h-3 w-3" />
            </Button>
          </Badge>
        </div>
      ))}
    </>
  );
}
