/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AsyncMultiSelect,
  type AsyncMultiSelectOption,
  Checkbox,
} from "@espressif/dashboard-ui-components/components";
import type { ThingGroupSelectorProps } from "./thing-group-selector.props";
import { useThingGroupSearch } from "./use-thing-group-search";
import { GroupOptionRow } from "./group-option-row";

const renderGroupOption = (opt: AsyncMultiSelectOption) => (
  <GroupOptionRow label={opt.label} secondaryText={opt.description} />
);

/**
 * The single group selector used across the dashboard. Built on
 * `AsyncMultiSelect` (single-select) so every page shares the same paginated,
 * debounced lookup and the same option rows. `topLevelOnly` and the optional
 * subgroup mode cover the register flow; `size="sm"` covers filter toolbars.
 */
export function ThingGroupSelector({
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
  topLevelOnly = false,
  allowSubgroupSelection = false,
  subgroupValue,
  onSubgroupSelect,
  subgroupLabel,
}: ThingGroupSelectorProps) {
  const { t } = useTranslation("common");
  const [wantsSubgroup, setWantsSubgroup] = useState(!!subgroupValue);

  useEffect(() => {
    if (!value && wantsSubgroup) {
      setWantsSubgroup(false);
    }
  }, [value, wantsSubgroup]);

  const {
    groups,
    setSearchInput,
    isLoading,
    hasMore,
    loadMore,
  } = useThingGroupSearch({ enabled: !disabled, onError, topLevelOnly });

  const showSubgroup = allowSubgroupSelection && !!value;

  const {
    groups: subgroups,
    setSearchInput: setSubgroupSearchInput,
    isLoading: isSubgroupsLoading,
    hasMore: hasMoreSubgroups,
    loadMore: loadMoreSubgroups,
  } = useThingGroupSearch({
    enabled: showSubgroup && wantsSubgroup,
    onError,
    parentGroupName: value,
  });

  const toOptions = (
    list: { groupName: string; groupId: string }[],
  ): AsyncMultiSelectOption[] =>
    list.map((g) => ({
      value: g.groupName,
      label: g.groupName,
      description: g.groupId,
    }));

  const groupOptions = useMemo(() => toOptions(groups), [groups]);
  const subgroupOptions = useMemo(() => toOptions(subgroups), [subgroups]);

  const handleGroupChange = (next: string | string[] | undefined) => {
    const groupName = typeof next === "string" ? next : undefined;
    setWantsSubgroup(false);
    onSubgroupSelect?.(undefined);
    onSelect(groupName);
  };

  const handleSubgroupToggle = (checked: boolean) => {
    setWantsSubgroup(checked);
    if (!checked && subgroupValue) {
      onSubgroupSelect?.(undefined);
    }
  };

  const handleSubgroupChange = (next: string | string[] | undefined) => {
    onSubgroupSelect?.(typeof next === "string" ? next : undefined);
  };

  return (
    <div className="flex flex-col gap-3">
      <AsyncMultiSelect
        multiple={false}
        value={value}
        onValueChange={handleGroupChange}
        options={groupOptions}
        onSearchChange={setSearchInput}
        isLoading={isLoading}
        hasMore={hasMore}
        onLoadMore={loadMore}
        label={label !== undefined ? label : t("thingGroupSelector.label", "Parent Group (optional)")}
        placeholder={placeholder ?? t("thingGroupSelector.placeholder", "Select parent group (optional)")}
        searchPlaceholder={
          searchPlaceholder ?? t("thingGroupSelector.searchPlaceholder", "Search node groups...")
        }
        emptyText={emptyText ?? t("thingGroupSelector.emptyText", "No node groups found.")}
        required={required}
        disabled={disabled}
        size={size}
        clearable={clearable}
        renderOption={renderGroupOption}
      />

      {showSubgroup && (
        <div className="flex flex-col gap-3">
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={wantsSubgroup}
              onCheckedChange={(next) => handleSubgroupToggle(next === true)}
            />
            <span>{t("thingGroupSelector.addToSubgroup", "Add to a subgroup")}</span>
          </label>

          {wantsSubgroup && (
            <AsyncMultiSelect
              multiple={false}
              value={subgroupValue}
              onValueChange={handleSubgroupChange}
              options={subgroupOptions}
              onSearchChange={setSubgroupSearchInput}
              isLoading={isSubgroupsLoading}
              hasMore={hasMoreSubgroups}
              onLoadMore={loadMoreSubgroups}
              label={subgroupLabel ?? t("thingGroupSelector.subgroupLabel", "Subgroup")}
              placeholder={t("thingGroupSelector.subgroupPlaceholder", "Select a subgroup")}
              searchPlaceholder={t("thingGroupSelector.subgroupSearchPlaceholder", "Search subgroups…")}
              emptyText={t("thingGroupSelector.subgroupEmptyText", "No subgroups found")}
              clearable
              renderOption={renderGroupOption}
            />
          )}
        </div>
      )}
    </div>
  );
}
