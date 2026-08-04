/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  AsyncMultiSelect,
  type AsyncMultiSelectOption,
} from "@espressif/dashboard-ui-components/components";
import type { ThingSelectorProps } from "./thing-selector.props";
import { useThingSearch } from "./use-thing-search";
import { ThingOptionRow } from "./thing-option-row";

const renderThingOption = (opt: AsyncMultiSelectOption) => (
  <ThingOptionRow label={opt.label} secondaryText={opt.description} />
);

/**
 * The single node/thing selector used across the dashboard. Built on
 * `AsyncMultiSelect` (single-select) so it shares the same paginated, debounced
 * lookup and the same rich option rows as `ThingGroupSelector`.
 */
export function ThingSelector({
  value,
  onSelect,
  onError,
  label,
  placeholder,
  searchPlaceholder,
  emptyText,
  required,
  disabled,
  clearable = true,
  size = "default",
}: ThingSelectorProps) {
  const { t } = useTranslation("ota-jobs");

  const { things, setSearchInput, isLoading, hasMore, loadMore } =
    useThingSearch({ enabled: !disabled, onError });

  const options = useMemo<AsyncMultiSelectOption[]>(
    () =>
      things.map((thing) => ({
        value: thing.thingName as string,
        label: thing.thingName as string,
        description: thing.thingId,
      })),
    [things],
  );

  return (
    <AsyncMultiSelect
      multiple={false}
      value={value}
      onValueChange={(next) =>
        onSelect(typeof next === "string" ? next : undefined)
      }
      options={options}
      onSearchChange={setSearchInput}
      isLoading={isLoading}
      hasMore={hasMore}
      onLoadMore={loadMore}
      label={label !== undefined ? label : t("thingSelector.label", "Node")}
      placeholder={placeholder ?? t("thingSelector.placeholder", "Select a node")}
      searchPlaceholder={
        searchPlaceholder ?? t("thingSelector.searchPlaceholder", "Search nodes...")
      }
      emptyText={emptyText ?? t("thingSelector.emptyText", "No nodes found.")}
      required={required}
      disabled={disabled}
      size={size}
      clearable={clearable}
      renderOption={renderThingOption}
    />
  );
}
