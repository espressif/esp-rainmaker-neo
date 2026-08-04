/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@espressif/dashboard-ui-components/components";
import type { RefObject } from "react";
import type {
  IndexField,
  InputStep,
  OperatorId,
} from "../advanced-indices-search.types";
import { FieldSelector } from "./field-selector";
import { OperatorSelector } from "./operator-selector";
import { ValueSelector } from "./value-selector";
import { ValueSuggestions } from "./value-suggestions";

interface SearchBarDropdownProps {
  step: InputStep;
  selectedField: IndexField | null;
  selectedOperator: OperatorId | null;
  fieldFilter: string;
  isLoading: boolean;
  fields: IndexField[];
  showDropdown: boolean;
  shouldFetchSuggestions: boolean;
  filteredSuggestions: { value: string; count: number }[];
  wrapperRef: RefObject<HTMLDivElement | null>;
  containerRef: RefObject<HTMLDivElement | null>;
  onDropdownChange: (open: boolean) => void;
  onFieldSelect: (field: IndexField) => void;
  onOperatorSelect: (operatorId: OperatorId) => void;
  onBooleanSelect: (val: "true" | "false") => void;
  onSuggestionSelect: (value: string) => void;
}

export function SearchBarDropdown({
  step,
  selectedField,
  fieldFilter,
  isLoading,
  fields,
  showDropdown,
  shouldFetchSuggestions,
  filteredSuggestions,
  wrapperRef,
  containerRef,
  onDropdownChange,
  onFieldSelect,
  onOperatorSelect,
  onBooleanSelect,
  onSuggestionSelect,
}: SearchBarDropdownProps) {
  return (
    <DropdownMenu
      open={showDropdown}
      onOpenChange={onDropdownChange}
      modal={false}
    >
      <DropdownMenuTrigger asChild>
        <div className="h-0 w-full" tabIndex={-1} aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        sideOffset={4}
        container={wrapperRef.current}
        className="w-[var(--radix-dropdown-menu-trigger-width)] max-h-64 overflow-y-auto rounded-xl"
        onCloseAutoFocus={(e) => e.preventDefault()}
        onFocusOutside={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => {
          if (containerRef.current?.contains(e.target as Node)) {
            e.preventDefault();
          }
        }}
      >
        {step === "field" && (
          <FieldSelector
            fields={fields}
            fieldFilter={fieldFilter}
            isLoading={isLoading}
            onSelect={onFieldSelect}
          />
        )}

        {step === "operator" && selectedField && (
          <OperatorSelector
            selectedField={selectedField}
            onSelect={onOperatorSelect}
          />
        )}

        {step === "value" && selectedField?.type === "Boolean" && (
          <ValueSelector onSelect={onBooleanSelect} />
        )}

        {shouldFetchSuggestions && filteredSuggestions.length > 0 && (
          <ValueSuggestions
            suggestions={filteredSuggestions}
            onSelect={onSuggestionSelect}
          />
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
