/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { describeThing, searchThings } from "@/aws/services/thing.service";
import { extractIparamsFields } from "@/aws/utils/iparams-fields";
import { useAuthStore } from "@/stores/auth.store";
import { trimOrNull, trimOrUndefined } from "@/utils/utils";

export interface ThingDetailsData {
  thingName: string;
  thingArn: string | undefined;
  thingGroupNames: string[];
  displayName: string | null;
  type: string | undefined;
  model: string | undefined;
  fwVersion: string | undefined;
  online: boolean | null;
  lastStatusTs: number | undefined;
  attributes: Record<string, unknown>;
}

interface ThingSearchData {
  thingName: string;
  thingGroupNames: string[];
  displayName: string | null;
  type: string | undefined;
  model: string | undefined;
  fwVersion: string | undefined;
  online: boolean | null;
  lastStatusTs: number | undefined;
}

export function useThingDetails(thingName: string | undefined) {
  const credentials = useAuthStore((s) => s.credentials);
  const enabled = !!thingName && !!credentials;

  const searchQuery = useQuery<ThingSearchData | null, Error>({
    queryKey: ["iot", "thing-search", thingName],
    queryFn: async (): Promise<ThingSearchData | null> => {
      if (!thingName) {
        return null;
      }
      const response = await searchThings({
        queryString: `thingName:${thingName}`,
        indexName: "AWS_Things",
        maxResults: 1,
      });
      const thing = response.things[0];
      if (!thing) {
        return null;
      }
      const fields = extractIparamsFields(thing.shadow);
      return {
        thingName: thing.thingName ?? "",
        thingGroupNames: thing.thingGroupNames ?? [],
        displayName: trimOrNull(fields.displayName),
        type: trimOrUndefined(fields.deviceType),
        model: trimOrUndefined(fields.deviceModel),
        fwVersion: trimOrUndefined(fields.fwVersion),
        online: fields.online,
        lastStatusTs: fields.lastSeen ?? undefined,
      };
    },
    enabled,
  });

  const describeQuery = useQuery({
    queryKey: ["iot", "thing-describe", thingName],
    queryFn: () => {
      if (!thingName) {
        return Promise.resolve(null);
      }
      return describeThing({ thingName });
    },
    enabled,
  });

  const data: ThingDetailsData | null = useMemo(() => {
    if (!searchQuery.data) {
      return null;
    }
    return {
      ...searchQuery.data,
      thingArn: describeQuery.data?.thingArn,
      attributes: describeQuery.data?.attributes ?? {},
    };
  }, [searchQuery.data, describeQuery.data]);

  return {
    data,
    isPending: searchQuery.isPending,
    isError: searchQuery.isError,
    isSuccess: searchQuery.isSuccess,
    error: searchQuery.error,
  };
}
