/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState, useCallback } from "react";
import {
  Dialog,
  DialogContent,
} from "@espressif/dashboard-ui-components/components";
import SearchBarContent from "./search-bar-content";
import type { AdvancedIndicesSearchProps } from "./advanced-indices-search.types";

const DEFAULT_MAX_CONDITIONS = 5;

export function AdvancedIndicesSearch({
  fields,
  query = "",
  maxAllowedConditions = DEFAULT_MAX_CONDITIONS,
  indexName = "AWS_Things",
  onSearch,
  children,
}: AdvancedIndicesSearchProps) {
  const [open, setOpen] = useState(false);

  const handleClose = useCallback(() => {
    setOpen(false);
  }, []);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {children}
      <DialogContent
        showCloseButton={false}
        className="sm:max-w-4xl p-0 gap-0 top-[30%] bg-transparent border-none shadow-none outline-none"
      >
        <SearchBarContent
          fields={fields}
          isLoading={false}
          query={query}
          maxAllowedConditions={maxAllowedConditions}
          indexName={indexName}
          onSearch={onSearch}
          onClose={handleClose}
        />
      </DialogContent>
    </Dialog>
  );
}
