/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { queryOptions, useQuery } from "@tanstack/react-query";
import { fetchEspUserAdminCreds, fetchRmngAdminCreds } from "./admin-creds.api";
import type { ScopedAwsCredentials, StepStack } from "./post-deployment.types";

export const adminCredsKeys = {
  all: ["admin-creds"] as const,
  stack: (stack: StepStack) => [...adminCredsKeys.all, stack] as const,
};

// Refetch a little before the ~1h STS session actually expires so long-lived
// dashboard sessions never sign a call with dead creds.
const STALE_MS = 45 * 60 * 1000;

export const adminCredsQueries = {
  /**
   * Scoped credentials for a stack. Both stacks vend ~1h STS sessions; keyed by
   * stack so the espuser and rmng credentials are cached independently.
   *
   * Exposed as options (rather than only as a hook) so a `queryFn` can pull
   * credentials through `queryClient.fetchQuery` instead of a component hook.
   * Use `fetchQuery`, never `ensureQueryData`: the latter serves cached data
   * regardless of `staleTime` and would hand back a nearly-expired session.
   */
  stack: (stack: StepStack) =>
    queryOptions<ScopedAwsCredentials>({
      queryKey: adminCredsKeys.stack(stack),
      queryFn: () =>
        stack === "espuser" ? fetchEspUserAdminCreds() : fetchRmngAdminCreds(),
      staleTime: STALE_MS,
      retry: 1,
    }),
};

/**
 * `enabled` is false for steps served by our own APIs, so viewing them never
 * mints credentials.
 */
export function useAdminCreds(stack: StepStack, enabled = true) {
  return useQuery({ ...adminCredsQueries.stack(stack), enabled });
}
