/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type {
  FieldType,
  IndexField,
  InputStep,
  OperatorId,
} from "../advanced-indices-search.types";
import { fieldLabel } from "../field-label";
import { OPERATORS_BY_TYPE } from "../operator-config";

/** Empty until an operator is picked; word-shaped operators carry a `labelKey`. */
function resolveOperatorLabel(
  fieldType: FieldType | undefined,
  operatorId: OperatorId | null,
  t: TFunction,
): string {
  if (!fieldType || !operatorId) {return "";}
  const operator = OPERATORS_BY_TYPE[fieldType]?.find((op) => op.id === operatorId);
  if (!operator) {return "";}
  return operator.labelKey ? t(operator.labelKey, operator.label) : operator.label;
}

interface StepInputProps {
  step: InputStep;
  selectedField: IndexField | null;
  selectedOperator: OperatorId | null;
  fieldFilter: string;
  valueInput: string;
  conditionsCount: number;
  canAddCondition: boolean;
  fieldFilterRef: React.RefObject<HTMLInputElement | null>;
  valueInputRef: React.RefObject<HTMLInputElement | null>;
  onFieldFilterChange: (value: string) => void;
  onValueInputChange: (value: string) => void;
  onValueSubmit: () => void;
  onResetInput: () => void;
}

export function StepInput({
  step,
  selectedField,
  selectedOperator,
  fieldFilter,
  valueInput,
  conditionsCount,
  canAddCondition,
  fieldFilterRef,
  valueInputRef,
  onFieldFilterChange,
  onValueInputChange,
  onValueSubmit,
  onResetInput,
}: StepInputProps) {
  const { t } = useTranslation("nodes");
  if (!canAddCondition) {return null;}

  const operatorLabel = resolveOperatorLabel(
    selectedField?.type,
    selectedOperator,
    t,
  );

  return (
    <div className="relative flex items-center gap-1 flex-1 min-w-48">
      {step === "field" && (
        <input
          ref={fieldFilterRef}
          type="text"
          value={fieldFilter}
          onChange={(e) => onFieldFilterChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.stopPropagation();
              onResetInput();
            }
          }}
          placeholder={t("advancedIndicesSearch.searchBarContent.selectFieldPlaceholder", "Select a field...")}
          className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          autoFocus
        />
      )}

      {step === "operator" && selectedField && (
        <span className="text-xs text-muted-foreground">
          <span className="font-medium">{fieldLabel(selectedField, t)}</span>
          <span className="mx-1">&rarr; {t("advancedIndicesSearch.searchBarContent.pickOperator", "pick operator")}</span>
        </span>
      )}

      {step === "value" && selectedField && (
        <ValueStepInput
          selectedField={selectedField}
          fieldName={fieldLabel(selectedField, t)}
          operatorLabel={operatorLabel}
          valueInput={valueInput}
          valueInputRef={valueInputRef}
          onValueInputChange={onValueInputChange}
          onValueSubmit={onValueSubmit}
          onResetInput={onResetInput}
          pickValueLabel={t("advancedIndicesSearch.searchBarContent.pickValue", "pick value ↓")}
          typeValuePlaceholder={t("advancedIndicesSearch.searchBarContent.typeValuePlaceholder", "Type a value...")}
        />
      )}

      {step === "idle" && (
        <span className="text-sm text-muted-foreground select-none">
          {conditionsCount === 0
            ? t("advancedIndicesSearch.searchBarContent.addFilterFirst", "Click to add a search filter...")
            : t("advancedIndicesSearch.searchBarContent.addAnotherFilter", "Add another filter...")}
        </span>
      )}
    </div>
  );
}

interface ValueStepInputProps {
  selectedField: IndexField;
  /** Already-translated display label for `selectedField`. */
  fieldName: string;
  operatorLabel: string;
  valueInput: string;
  valueInputRef: React.RefObject<HTMLInputElement | null>;
  onValueInputChange: (value: string) => void;
  onValueSubmit: () => void;
  onResetInput: () => void;
  pickValueLabel: string;
  typeValuePlaceholder: string;
}

function ValueStepInput({
  selectedField,
  fieldName,
  operatorLabel,
  valueInput,
  valueInputRef,
  onValueInputChange,
  onValueSubmit,
  onResetInput,
  pickValueLabel,
  typeValuePlaceholder,
}: ValueStepInputProps) {
  if (selectedField.type === "Boolean") {
    return (
      <div className="flex items-center gap-1 flex-1">
        <span className="text-xs text-muted-foreground font-medium">
          {fieldName}
        </span>
        <span className="text-xs font-semibold text-primary">
          {operatorLabel}
        </span>
        <span className="text-xs text-muted-foreground ml-1">
          {pickValueLabel}
        </span>
      </div>
    );
  }

  const inputType = selectedField.type === "Number" ? "number" : "text";

  return (
    <div className="flex items-center gap-1 flex-1">
      <span className="text-xs text-muted-foreground font-medium">
        {fieldName}
      </span>
      <span className="text-xs text-muted-foreground font-medium">{operatorLabel}</span>
      <input
        ref={valueInputRef}
        type={inputType}
        value={valueInput}
        onChange={(e) => onValueInputChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            e.stopPropagation();
            onValueSubmit();
          }
          if (e.key === "Escape") {
            e.stopPropagation();
            onResetInput();
          }
        }}
        placeholder={typeValuePlaceholder}
        className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        autoFocus
      />
    </div>
  );
}
