/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@/stores/auth.store";
import { listThings, searchThings } from "@/aws/services/thing.service";
import { useTokenPagination } from "@/hooks/use-token-pagination";
import {
  extractIparamsFields,
  type IparamsFields,
} from "@/pages/home/node-management/nodes/iparams-fields";

export interface AwsThingRow {
  thingName: string;
  thingId: string | null;
  displayName: string | null;
  online: boolean | null;
  deviceType: string | null;
  lastSeen: number | null;
}

interface PrimaryThing {
  thingName: string;
  thingId: string | null;
  fields: IparamsFields | null;
  connectivityTs: number | null;
}

interface EnrichmentEntry extends IparamsFields {
  connectivityTs: number | null;
}

interface UseAwsThingsListParams {
  maxResults: number;
}

function normalizeLastSeen(
  fields: IparamsFields | null | undefined,
  connectivityTs: number | null,
): number | null {
  if (fields?.lastSeen != null) {
    return fields.lastSeen;
  }
  if (connectivityTs != null) {
    return Math.floor(connectivityTs / 1000);
  }
  return null;
}

function buildRow(
  thingName: string,
  thingId: string | null,
  fields: IparamsFields | null | undefined,
  connectivityTs: number | null,
): AwsThingRow {
  return {
    thingName,
    thingId,
    displayName: fields?.displayName ?? null,
    online: fields?.online ?? null,
    deviceType: fields?.deviceType ?? null,
    lastSeen: normalizeLastSeen(fields, connectivityTs),
  };
}

function toRow(
  thing: PrimaryThing,
  isSearch: boolean,
  enrichment: Record<string, EnrichmentEntry>,
): AwsThingRow {
  if (isSearch) {
    return buildRow(thing.thingName, thing.thingId, thing.fields, thing.connectivityTs);
  }
  const enriched = enrichment[thing.thingName];
  return buildRow(thing.thingName, null, enriched, enriched?.connectivityTs ?? null);
}

export function useAwsThingsList({ maxResults }: UseAwsThingsListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  const [searchQuery, setSearchQuery] = useState("");

  const {
    state: pagination,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  } = useTokenPagination(maxResults);

  const trimmed = searchQuery.trim();
  const isSearch = trimmed.length > 0;

  const listQuery = useQuery<{ things: PrimaryThing[]; nextToken?: string }>({
    queryKey: [
      "iot",
      "aws-things-list",
      isSearch ? "search" : "list",
      trimmed,
      pagination.pageSize,
      pagination.nextToken,
    ],
    queryFn: async () => {
      if (isSearch) {
        const response = await searchThings({
          indexName: "AWS_Things",
          queryString: `thingName:${trimmed}*`,
          maxResults: pagination.pageSize,
          nextToken: pagination.nextToken,
        });
        return {
          things: response.things.map(
            (thing): PrimaryThing => ({
              thingName: thing.thingName ?? "",
              thingId: thing.thingId ?? null,
              fields: extractIparamsFields(thing.shadow),
              connectivityTs: thing.connectivity?.timestamp ?? null,
            }),
          ),
          nextToken: response.nextToken,
        };
      }

      const response = await listThings({
        maxResults: pagination.pageSize,
        nextToken: pagination.nextToken,
      });
      return {
        things: response.things.map(
          (thing): PrimaryThing => ({
            thingName: thing.thingName ?? "",
            thingId: null,
            fields: null,
            connectivityTs: null,
          }),
        ),
        nextToken: response.nextToken,
      };
    },
    enabled: !!credentials,
    placeholderData: keepPreviousData,
  });

  const primaryThings = useMemo(
    () => listQuery.data?.things ?? [],
    [listQuery.data?.things],
  );

  const enrichmentNames = useMemo(() => {
    if (isSearch) {
      return [] as string[];
    }
    return primaryThings.map((t) => t.thingName).filter((name) => !!name);
  }, [isSearch, primaryThings]);

  const enrichmentQuery = useQuery<Record<string, EnrichmentEntry>>({
    queryKey: ["iot", "aws-things-list-enrichment", enrichmentNames],
    queryFn: async () => {
      const queryString = enrichmentNames
        .map((name) => `thingName:"${name}"`)
        .join(" OR ");
      const response = await searchThings({
        indexName: "AWS_Things",
        queryString: `(${queryString})`,
        maxResults: enrichmentNames.length,
      });
      const map: Record<string, EnrichmentEntry> = {};
      for (const thing of response.things) {
        const key = thing.thingName;
        if (!key) {
          continue;
        }
        map[key] = {
          ...extractIparamsFields(thing.shadow),
          connectivityTs: thing.connectivity?.timestamp ?? null,
        };
      }
      return map;
    },
    enabled: !!credentials && enrichmentNames.length > 0,
    retry: false,
    staleTime: 30_000,
  });

  const rows: AwsThingRow[] = useMemo(() => {
    const enrichment = enrichmentQuery.data ?? {};
    return primaryThings.map((thing) => toRow(thing, isSearch, enrichment));
  }, [primaryThings, enrichmentQuery.data, isSearch]);

  const hasNextPage = !!listQuery.data?.nextToken;

  const handleSearch = (value: string) => {
    setSearchQuery(value);
    resetPagination();
  };

  const handleClearSearch = () => {
    setSearchQuery("");
    resetPagination();
  };

  const handleNextPage = () => goNext(listQuery.data?.nextToken);

  return {
    pagination,
    rows,
    isLoading: listQuery.isLoading,
    isFetching: listQuery.isFetching || enrichmentQuery.isFetching,
    error: listQuery.error,
    refetch: listQuery.refetch,
    hasNextPage,
    hasPrevPage,
    handlePageSizeChange,
    handleNextPage,
    handlePrevPage: goPrev,
    handleSearch,
    handleClearSearch,
  };
}
