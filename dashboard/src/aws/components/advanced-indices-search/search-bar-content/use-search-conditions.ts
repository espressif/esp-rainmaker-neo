/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState, useCallback } from "react";
import type {
  IndexField,
  OperatorId,
  Junction,
  SearchCondition,
} from "../advanced-indices-search.types";

export function useSearchConditions(maxAllowedConditions: number) {
  const [conditions, setConditions] = useState<SearchCondition[]>([]);
  const [junctions, setJunctions] = useState<Junction[]>([]);

  const canAddCondition = conditions.length < maxAllowedConditions;

  const addCondition = useCallback(
    (field: IndexField, operator: OperatorId, value: string) => {
      const newCondition: SearchCondition = {
        field: field.name,
        fieldType: field.type,
        operator,
        value,
      };

      setConditions((prev) => {
        const next = [...prev, newCondition];
        if (prev.length > 0) {
          setJunctions((j) => [...j, "AND"]);
        }
        return next;
      });
    },
    [],
  );

  const removeCondition = useCallback((index: number) => {
    setConditions((prev) => prev.filter((_, i) => i !== index));
    setJunctions((prev) => {
      const jIdx = index === 0 ? 0 : index - 1;
      return prev.filter((_, i) => i !== jIdx);
    });
  }, []);

  const toggleJunction = useCallback((index: number) => {
    setJunctions((prev) =>
      prev.map((j, i) => {
        if (i !== index) {
          return j;
        }
        return j === "AND" ? "OR" : "AND";
      }),
    );
  }, []);

  const clearConditions = useCallback(() => {
    setConditions([]);
    setJunctions([]);
  }, []);

  const setParsedConditions = useCallback(
    (parsedConditions: SearchCondition[], parsedJunctions: Junction[]) => {
      setConditions(parsedConditions);
      setJunctions(parsedJunctions);
    },
    [],
  );

  return {
    conditions,
    junctions,
    canAddCondition,
    addCondition,
    removeCondition,
    toggleJunction,
    clearConditions,
    setParsedConditions,
  };
}
