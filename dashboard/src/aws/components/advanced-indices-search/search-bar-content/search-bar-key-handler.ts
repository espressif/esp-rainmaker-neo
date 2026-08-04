/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { InputStep } from "../advanced-indices-search.types";

interface SearchBarKeyDownOptions {
  step: InputStep;
  conditionsLength: number;
  handleSearch: () => void;
  resetInput: () => void;
  removeCondition: (index: number) => void;
}

export function createSearchBarKeyDownHandler({
  step,
  conditionsLength,
  handleSearch,
  resetInput,
  removeCondition,
}: SearchBarKeyDownOptions) {
  return (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && step === "idle" && conditionsLength > 0) {
      e.preventDefault();
      handleSearch();
    }
    if (e.key === "Escape" && step !== "idle") {
      resetInput();
    }
    if (e.key === "Backspace") {
      if (step === "operator" || step === "value") {
        e.preventDefault();
        resetInput();
      } else if (step === "idle" && conditionsLength > 0) {
        removeCondition(conditionsLength - 1);
      }
    }
  };
}
