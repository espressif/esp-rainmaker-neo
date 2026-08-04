/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useState } from "react";

const DEBOUNCE_MS = 300;

export function useDebouncedSearch(delayMs: number = DEBOUNCE_MS) {
  const [searchInput, setSearchInput] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchInput.trim());
    }, delayMs);
    return () => clearTimeout(timer);
  }, [searchInput, delayMs]);

  return { debouncedSearch, setSearchInput };
}
