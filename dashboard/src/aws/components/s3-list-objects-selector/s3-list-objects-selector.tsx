/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@/stores/auth.store";
import { listS3Objects, type S3Object } from "@/aws/services/s3.service";
import { useTranslation } from "react-i18next";
import { AsyncMultiSelect } from "@espressif/dashboard-ui-components/components";
import { useDebouncedSearch } from "./use-debounced-search";
import type {
  S3ListObjectsSelectorProps,
  S3Option,
} from "./s3-list-objects-selector.props";

function toKeys(value: string | string[] | undefined): string[] {
  if (typeof value === "string") {
    return [value];
  }
  return Array.isArray(value) ? value : [];
}

export function S3ListObjectsSelector({
  value,
  onSelect,
  onError,
  bucket,
  prefix,
  maxKeys,
  listType,
  region,
  multiple,
  formatOption,
  renderOption,
  label,
  placeholder,
  resolveValueOnLoad,
}: S3ListObjectsSelectorProps) {
  const { t } = useTranslation("ota-jobs");
  const credentials = useAuthStore((s) => s.credentials);
  const resolvedLabel = label ?? t("s3ListObjectsSelector.label", "Object");
  // Debounced so typing doesn't re-filter/re-render the full object list on
  // every keystroke (the list isn't paginated — it can be large).
  const { debouncedSearch, setSearchInput } = useDebouncedSearch();

  const { data, isLoading, error, isFetching } = useQuery({
    queryKey: ["s3", "s3-list-objects-selector", bucket, prefix, maxKeys, listType, region],
    queryFn: () => listS3Objects({ bucket, prefix, maxKeys, listType, region }),
    enabled: !!credentials,
  });

  const objects = useMemo(() => data ?? [], [data]);
  const objectsByKey = useMemo(
    () => new Map(objects.map((o) => [o.key, o])),
    [objects]
  );

  const toOption = useCallback(
    (o: S3Object) =>
      formatOption?.(o) ?? {
        value: o.key,
        label: prefix && o.key.startsWith(prefix) ? o.key.slice(prefix.length) : o.key,
      },
    [formatOption, prefix]
  );

  const options = useMemo(() => {
    const query = debouncedSearch.toLowerCase();
    return objects
      .map(toOption)
      .filter((opt) => !query || opt.label.toLowerCase().includes(query));
  }, [objects, toOption, debouncedSearch]);

  // Hand the consumer's renderOption both the mapped option and the source S3
  // object (resolved by key) so it can design rich rows without the selector
  // knowing anything about the resource.
  const renderOptionWithObject = useMemo(
    () =>
      renderOption
        ? (option: S3Option) => renderOption(option, objectsByKey.get(option.value))
        : undefined,
    [renderOption, objectsByKey]
  );

  const handleError = useCallback(
    (err: Error) => {
      onError(err);
    },
    [onError]
  );

  const handleSelect = useCallback(
    (next: string | string[] | undefined) => {
      if (next === undefined) {
        onSelect(undefined);
        return;
      }
      const keys = Array.isArray(next) ? next : [next];
      const selected = keys
        .map((k) => objectsByKey.get(k))
        .filter((o): o is S3Object => !!o);
      onSelect(next, selected);
    },
    [onSelect, objectsByKey]
  );

  useEffect(() => {
    if (error) {
      handleError(error instanceof Error ? error : new Error(String(error)));
    }
  }, [error, handleError]);

  // One-shot reconciliation of a preset `value` (e.g. from a deep link) once the
  // list first settles: emit the full selection when the key exists, or clear it
  // when it doesn't (so an unresolved key never lingers in the field).
  const hasResolvedPresetRef = useRef(false);
  useEffect(() => {
    if (!resolveValueOnLoad || hasResolvedPresetRef.current) {
      return;
    }
    const hasSettled = !isFetching && (data !== undefined || error != null);
    if (!hasSettled) {
      return;
    }
    hasResolvedPresetRef.current = true;
    const keys = toKeys(value);
    if (keys.length === 0) {
      return;
    }
    const resolved = keys
      .map((k) => objectsByKey.get(k))
      .filter((o): o is S3Object => !!o);
    onSelect(resolved.length > 0 ? value : undefined, resolved);
  }, [
    resolveValueOnLoad,
    isFetching,
    data,
    error,
    value,
    objectsByKey,
    onSelect,
  ]);

  return (
    <AsyncMultiSelect
      value={value}
      onValueChange={handleSelect}
      options={options}
      onSearchChange={setSearchInput}
      isLoading={isLoading || isFetching}
      multiple={multiple ?? false}
      renderOption={renderOptionWithObject}
      placeholder={placeholder ?? t("s3ListObjectsSelector.placeholder", "Select an object")}
      emptyText={t("s3ListObjectsSelector.emptyText", "No objects found.")}
      searchPlaceholder={t("s3ListObjectsSelector.searchPlaceholder", "Search objects...")}
      clearable
      label={resolvedLabel}
    />
  );
}
