/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { describeThing, searchThings } from "@/aws/services/thing.service";
import { useAuthStore } from "@/stores/auth.store";

export interface ThingDetailsData {
  thingName: string;
  thingId: string | undefined;
  thingArn: string | undefined;
  thingGroupNames: string[];
  displayName: string;
  type: string | undefined;
  model: string | undefined;
  fwVersion: string | undefined;
  online: boolean | null;
  lastStatusTs: number | undefined;
  attributes: Record<string, unknown>;
}

interface ThingSearchData {
  thingName: string;
  thingId: string | undefined;
  thingGroupNames: string[];
  displayName: string;
  type: string | undefined;
  model: string | undefined;
  fwVersion: string | undefined;
  online: boolean | null;
  lastStatusTs: number | undefined;
}

interface ShadowDeviceInfo {
  name?: string;
  type?: string;
  model?: string;
  fw_version?: string;
}

interface ShadowIparamsReported {
  data?: {
    device?: { t?: ShadowDeviceInfo };
  };
  online?: boolean;
  disconnect_info?: {
    last_disconnect_ts?: number;
  };
}

interface ShadowIparamsMetadata {
  reported?: {
    online?: { timestamp?: number };
  };
}

interface ShadowIparams {
  reported?: ShadowIparamsReported;
  metadata?: ShadowIparamsMetadata;
}

const EMPTY_PARSED_SHADOW: {
  device: ShadowDeviceInfo;
  online: boolean | null;
  lastStatusTs: number | undefined;
} = { device: {}, online: null, lastStatusTs: undefined };

function pickLastStatusTs(iparams: ShadowIparams): number | undefined {
  const metadataTs = iparams.metadata?.reported?.online?.timestamp;
  if (typeof metadataTs === "number") {
    return metadataTs;
  }
  const disconnectTs = iparams.reported?.disconnect_info?.last_disconnect_ts;
  if (typeof disconnectTs === "number") {
    return Math.floor(disconnectTs / 1000);
  }
  return undefined;
}

function pickOnline(iparams: ShadowIparams): boolean | null {
  const online = iparams.reported?.online;
  return typeof online === "boolean" ? online : null;
}

function parseShadow(shadow: string | undefined): typeof EMPTY_PARSED_SHADOW {
  if (!shadow) {
    return EMPTY_PARSED_SHADOW;
  }
  try {
    const parsed = JSON.parse(shadow) as {
      name?: { iparams?: ShadowIparams };
    };
    const iparams = parsed?.name?.iparams ?? {};
    return {
      device: iparams.reported?.data?.device?.t ?? {},
      online: pickOnline(iparams),
      lastStatusTs: pickLastStatusTs(iparams),
    };
  } catch {
    return EMPTY_PARSED_SHADOW;
  }
}

function trimOrUndefined(value: string | undefined): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
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
      const { device, online, lastStatusTs } = parseShadow(thing.shadow);
      const displayName = trimOrUndefined(device.name) ?? thing.thingName ?? "";
      return {
        thingName: thing.thingName ?? "",
        thingId: thing.thingId,
        thingGroupNames: thing.thingGroupNames ?? [],
        displayName,
        type: trimOrUndefined(device.type),
        model: trimOrUndefined(device.model),
        fwVersion: trimOrUndefined(device.fw_version),
        online,
        lastStatusTs,
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
