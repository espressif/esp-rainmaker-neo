/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useQuery } from "@tanstack/react-query";
import type { IndexField, InputStep, OperatorId } from "../advanced-indices-search.types";
import { getFieldValues } from "@/aws/services/thing.service";

export function useSearchBarSuggestions({
  step,
  selectedField,
  selectedOperator,
  valueInput,
  indexName,
}: {
  step: InputStep;
  selectedField: IndexField | null;
  selectedOperator: OperatorId | null;
  valueInput: string;
  indexName: string;
}) {
  const shouldFetchSuggestions = Boolean(
    step === "value" &&
    selectedField?.type === "String" &&
    selectedOperator &&
    ["eq", "neq"].includes(selectedOperator),
  );

  const { data: valueSuggestions } = useQuery({
    queryKey: ["iot", "field-values", indexName, selectedField?.name],
    queryFn: () => {
      if (!selectedField) {
        throw new Error("selectedField is required");
      }
      return getFieldValues(selectedField.name, "*", indexName);
    },
    enabled: !!shouldFetchSuggestions && !!selectedField,
    retry: false,
    staleTime: 1000 * 60 * 5,
  });

  const filteredSuggestions = (valueSuggestions ?? []).filter(
    (s) =>
      !valueInput ||
      s.value.toLowerCase().includes(valueInput.toLowerCase()),
  );

  const showDropdown = Boolean(
    step === "field" ||
    step === "operator" ||
    (step === "value" && selectedField?.type === "Boolean") ||
    (shouldFetchSuggestions && filteredSuggestions.length > 0),
  );

  return {
    shouldFetchSuggestions,
    filteredSuggestions,
    showDropdown,
  };
}
