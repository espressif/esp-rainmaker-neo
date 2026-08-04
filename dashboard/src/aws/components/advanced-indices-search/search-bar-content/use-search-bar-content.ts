/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState, useRef, useCallback, useEffect } from "react";
import type { IndexField, InputStep, OperatorId } from "../advanced-indices-search.types";
import { OPERATORS_BY_TYPE } from "../operator-config";
import { buildQueryString } from "../query-builder";
import { parseQueryString } from "../query-parser";
import { useSearchConditions } from "./use-search-conditions";
import { createSearchBarKeyDownHandler } from "./search-bar-key-handler";
import { useSearchBarSuggestions } from "./use-search-bar-suggestions";

interface UseSearchBarContentOptions {
  fields: IndexField[];
  query?: string;
  maxAllowedConditions: number;
  indexName?: string;
  onSearch: (query: string) => void;
  onClose: () => void;
}

export function useSearchBarContent({
  fields,
  query = "",
  maxAllowedConditions,
  indexName = "AWS_Things",
  onSearch,
  onClose,
}: UseSearchBarContentOptions) {
  const {
    conditions,
    junctions,
    canAddCondition,
    addCondition: appendCondition,
    removeCondition,
    toggleJunction,
    clearConditions,
    setParsedConditions,
  } = useSearchConditions(maxAllowedConditions);

  const [step, setStep] = useState<InputStep>("idle");
  const [selectedField, setSelectedField] = useState<IndexField | null>(null);
  const [selectedOperator, setSelectedOperator] = useState<OperatorId | null>(null);
  const [valueInput, setValueInput] = useState("");
  const [fieldFilter, setFieldFilter] = useState("");

  const valueInputRef = useRef<HTMLInputElement>(null);
  const fieldFilterRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (query && fields.length > 0) {
      const parsed = parseQueryString(query, fields);
      if (parsed.conditions.length > 0) {
        setParsedConditions(parsed.conditions, parsed.junctions);
      }
    }
  }, [query, fields, setParsedConditions]);

  const resetInput = useCallback(() => {
    setStep("idle");
    setSelectedField(null);
    setSelectedOperator(null);
    setValueInput("");
    setFieldFilter("");
  }, []);

  const addCondition = useCallback(
    (field: IndexField, operator: OperatorId, value: string) => {
      appendCondition(field, operator, value);
      resetInput();
    },
    [appendCondition, resetInput],
  );

  const handleSearch = useCallback(() => {
    const validConditions = conditions.filter(
      (c) =>
        c.field &&
        c.operator &&
        (OPERATORS_BY_TYPE[c.fieldType]?.find((op) => op.id === c.operator)
          ?.noValue ||
          c.value),
    );
    onSearch(buildQueryString(validConditions, junctions));
    onClose();
  }, [conditions, junctions, onSearch, onClose]);

  const handleFieldSelect = useCallback((field: IndexField) => {
    setSelectedField(field);
    setFieldFilter("");
    setStep("operator");
  }, []);

  const handleOperatorSelect = useCallback(
    (operatorId: OperatorId) => {
      if (!selectedField) {return;}

      const op = OPERATORS_BY_TYPE[selectedField.type]?.find(
        (o) => o.id === operatorId,
      );

      if (op?.noValue) {
        addCondition(selectedField, operatorId, "");
        return;
      }

      setSelectedOperator(operatorId);
      setStep("value");
      requestAnimationFrame(() => valueInputRef.current?.focus());
    },
    [selectedField, addCondition],
  );

  const handleValueSubmit = useCallback(() => {
    if (!selectedField || !selectedOperator) {return;}
    const trimmed = valueInput.trim();
    if (!trimmed) {return;}
    addCondition(selectedField, selectedOperator, trimmed);
  }, [selectedField, selectedOperator, valueInput, addCondition]);

  const handleBooleanSelect = useCallback(
    (val: "true" | "false") => {
      if (!selectedField || !selectedOperator) {return;}
      addCondition(selectedField, selectedOperator, val);
    },
    [selectedField, selectedOperator, addCondition],
  );

  const startFieldSelect = useCallback(() => {
    if (!canAddCondition) {return;}
    setStep("field");
    requestAnimationFrame(() => fieldFilterRef.current?.focus());
  }, [canAddCondition]);

  const { shouldFetchSuggestions, filteredSuggestions, showDropdown } =
    useSearchBarSuggestions({
      step,
      selectedField,
      selectedOperator,
      valueInput,
      indexName,
    });

  const handleKeyDown = (e: React.KeyboardEvent) => {
    createSearchBarKeyDownHandler({
      step,
      conditionsLength: conditions.length,
      handleSearch,
      resetInput,
      removeCondition,
    })(e);
  };

  const handleDropdownChange = useCallback(
    (open: boolean) => {
      if (!open) {resetInput();}
    },
    [resetInput],
  );

  const handleClear = useCallback(() => {
    clearConditions();
    resetInput();
  }, [clearConditions, resetInput]);

  const handleSuggestionSelect = useCallback(
    (value: string) => {
      if (!selectedField || !selectedOperator) {return;}
      addCondition(selectedField, selectedOperator, value);
    },
    [selectedField, selectedOperator, addCondition],
  );

  const handleBarClick = useCallback(() => {
    if (step === "idle" && canAddCondition) {
      startFieldSelect();
    }
  }, [step, canAddCondition, startFieldSelect]);

  return {
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
  };
}
