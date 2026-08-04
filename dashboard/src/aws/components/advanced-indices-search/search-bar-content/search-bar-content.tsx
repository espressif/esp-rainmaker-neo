/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import type {
  IndexField,
} from "../advanced-indices-search.types";
import { ConditionChips } from "./condition-chips";
import { StepInput } from "./step-input";
import { SearchBarActions } from "./search-bar-actions";
import { SearchBarDropdown } from "./search-bar-dropdown";
import { useSearchBarContent } from "./use-search-bar-content";

interface SearchBarContentProps {
  fields: IndexField[];
  isLoading: boolean;
  query?: string;
  maxAllowedConditions: number;
  indexName?: string;
  onSearch: (query: string) => void;
  onClose: () => void;
}

export default function SearchBarContent({
  fields,
  isLoading,
  query = "",
  maxAllowedConditions,
  indexName = "AWS_Things",
  onSearch,
  onClose,
}: SearchBarContentProps) {
  const { t } = useTranslation("nodes");
  const {
    conditions,
    junctions,
    step,
    selectedField,
    selectedOperator,
    fieldFilter,
    valueInput,
    canAddCondition,
    valueInputRef,
    fieldFilterRef,
    containerRef,
    wrapperRef,
    shouldFetchSuggestions,
    filteredSuggestions,
    showDropdown,
    setFieldFilter,
    setValueInput,
    toggleJunction,
    removeCondition,
    handleSearch,
    handleKeyDown,
    handleFieldSelect,
    handleOperatorSelect,
    handleValueSubmit,
    handleBooleanSelect,
    handleDropdownChange,
    handleClear,
    handleSuggestionSelect,
    handleBarClick,
    resetInput,
  } = useSearchBarContent({
    fields,
    query,
    maxAllowedConditions,
    indexName,
    onSearch,
    onClose,
  });

  return (
    <div ref={wrapperRef} className="flex flex-col" onKeyDown={handleKeyDown}>
      <div
        ref={containerRef}
        className="relative flex items-center gap-3 rounded-xl border bg-background p-4 min-h-12 cursor-text"
        onClick={handleBarClick}
      >
        <div className="flex flex-1 items-center gap-2 flex-wrap min-w-0">
          <ConditionChips
            conditions={conditions}
            junctions={junctions}
            fields={fields}
            onToggleJunction={toggleJunction}
            onRemoveCondition={removeCondition}
          />

          <StepInput
            step={step}
            selectedField={selectedField}
            selectedOperator={selectedOperator}
            fieldFilter={fieldFilter}
            valueInput={valueInput}
            conditionsCount={conditions.length}
            canAddCondition={canAddCondition}
            fieldFilterRef={fieldFilterRef}
            valueInputRef={valueInputRef}
            onFieldFilterChange={setFieldFilter}
            onValueInputChange={setValueInput}
            onValueSubmit={handleValueSubmit}
            onResetInput={resetInput}
          />

          {!canAddCondition && conditions.length > 0 && step === "idle" && (
            <span className="text-xs text-muted-foreground ml-1">
              {t(
                "advancedIndicesSearch.searchBarContent.maxFiltersReached",
                "Max filters reached",
              )}
            </span>
          )}
        </div>

        <SearchBarActions
          disabled={conditions.length === 0}
          onClear={handleClear}
          onSearch={handleSearch}
        />
      </div>

      <SearchBarDropdown
        step={step}
        selectedField={selectedField}
        selectedOperator={selectedOperator}
        fieldFilter={fieldFilter}
        isLoading={isLoading}
        fields={fields}
        showDropdown={showDropdown}
        shouldFetchSuggestions={!!shouldFetchSuggestions}
        filteredSuggestions={filteredSuggestions}
        wrapperRef={wrapperRef}
        containerRef={containerRef}
        onDropdownChange={handleDropdownChange}
        onFieldSelect={handleFieldSelect}
        onOperatorSelect={handleOperatorSelect}
        onBooleanSelect={handleBooleanSelect}
        onSuggestionSelect={handleSuggestionSelect}
      />
    </div>
  );
}
