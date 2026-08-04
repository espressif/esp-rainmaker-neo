/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  DropdownMenuGroup,
  DropdownMenuItem,
  Badge,
} from "@espressif/dashboard-ui-components/components";
import type { IndexField, OperatorId } from "../advanced-indices-search.types";
import { OPERATORS_BY_TYPE } from "../operator-config";

interface OperatorSelectorProps {
  selectedField: IndexField;
  onSelect: (operatorId: OperatorId) => void;
}

export function OperatorSelector({
  selectedField,
  onSelect,
}: OperatorSelectorProps) {
  const { t } = useTranslation("common");
  const operators = OPERATORS_BY_TYPE[selectedField.type] ?? [];

  return (
    <DropdownMenuGroup>
      {operators.map((op) => (
        <DropdownMenuItem
          key={op.id}
          className="justify-between"
          onSelect={(e) => {
            e.preventDefault();
            onSelect(op.id);
          }}
        >
          <span className="text-sm text-foreground">
            {t(op.descriptionKey, op.description)}
          </span>
          <Badge
            variant="outline"
            className="shrink-0 gap-1 text-[9px] tracking-wide font-normal text-muted-foreground uppercase rounded-md p-1 font-mono"
          >
            {op.labelKey ? t(op.labelKey, op.label) : op.label}
          </Badge>
        </DropdownMenuItem>
      ))}
    </DropdownMenuGroup>
  );
}
