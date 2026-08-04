/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { describeThing } from "@/aws/services/thing.service";
import { useAuthStore } from "@/stores/auth.store";

export function useThingAttributes(thingName: string | undefined) {
  const credentials = useAuthStore((s) => s.credentials);
  const enabled = !!thingName && !!credentials;

  const query = useQuery({
    queryKey: ["iot", "thing-describe", thingName],
    queryFn: () => {
      if (!thingName) {
        return Promise.resolve(null);
      }
      return describeThing({ thingName });
    },
    enabled,
  });

  const attributes = useMemo<Record<string, string>>(
    () => query.data?.attributes ?? {},
    [query.data],
  );

  return {
    attributes,
    isLoading: query.isPending,
    isError: query.isError,
    isRefetching: query.isRefetching,
    error: query.error,
    refetch: query.refetch,
  };
}
