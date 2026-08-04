/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import {
  queryOptions,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { ReadLimitOptions } from "@/aws/services/deployment-limits.service";
import { adminCredsKeys, adminCredsQueries } from "./admin-creds.queries";
import type { ScopedAwsCredentials, StepStack } from "./post-deployment.types";

export const deploymentValuesKeys = {
  all: ["post-deployment", "values"] as const,
  value: (id: string) => [...deploymentValuesKeys.all, id] as const,
};

/**
 * The credentials call failed, as opposed to the AWS read itself — the two need
 * different copy, since one is a deployment/permissions problem and the other is
 * usually a stale session.
 */
export class CredentialsUnavailableError extends Error {
  constructor(
    readonly stack: StepStack,
    /** The underlying credentials failure, kept for logging — never rendered. */
    readonly reason?: unknown,
  ) {
    super("Scoped AWS credentials unavailable");
    this.name = "CredentialsUnavailableError";
  }
}

// Short enough that an operator who has just been granted production access sees
// it on their next visit, long enough that switching tabs is not a refetch.
const READING_STALE_MS = 5 * 60 * 1000;
const READING_GC_MS = 30 * 60 * 1000;

/**
 * The minimum a value has to expose to be readable. Structural, so the page's
 * own config satisfies it without the API layer depending on the page.
 */
export interface DeploymentValueQuerySpec<TReading> {
  id: string;
  stack: StepStack;
  read: (
    creds: ScopedAwsCredentials,
    options?: ReadLimitOptions,
  ) => Promise<TReading>;
}

export const deploymentValuesQueries = {
  /**
   * Credentials are pulled inside the `queryFn` rather than through a component
   * hook. A disabled `useQuery` still observes its query, so gating a read on
   * `useAwsClients(...)` would leak a shared credentials failure into every card
   * that happened to name the same stack — and leave the read stuck `pending`
   * forever, because `enabled: false` never resolves. Fetching here gives each
   * value one honest loading state and one honest error, while the three
   * `espuser` values still dedupe onto a single in-flight credentials request.
   */
  reading: <TReading,>(value: DeploymentValueQuerySpec<TReading>) =>
    queryOptions({
      queryKey: deploymentValuesKeys.value(value.id),
      queryFn: async ({ client, signal }) => {
        const creds = await client
          .fetchQuery(adminCredsQueries.stack(value.stack))
          .catch((reason: unknown) => {
            throw new CredentialsUnavailableError(value.stack, reason);
          });
        return value.read(creds, { signal });
      },
      staleTime: READING_STALE_MS,
      gcTime: READING_GC_MS,
      // An access-denied or unpermitted call will not fix itself, and every retry
      // would re-enter fetchQuery and re-mint credentials.
      retry: false,
    }),
};

export function useDeploymentValueReading<TReading>(
  value: DeploymentValueQuerySpec<TReading>,
) {
  return useQuery(deploymentValuesQueries.reading(value));
}

/**
 * Retry that covers both failure modes with one action: expire the cached
 * credentials so the next read mints a fresh session, then re-run the read.
 *
 * Invalidates rather than removes the credentials — `useSmsSandbox` observes the
 * espuser query, and removing it out from under that card would reset it.
 */
export function useRefreshDeploymentValue() {
  const queryClient = useQueryClient();

  return useCallback(
    async (value: { id: string; stack: StepStack }) => {
      await queryClient.invalidateQueries({
        queryKey: adminCredsKeys.stack(value.stack),
      });
      await queryClient.refetchQueries({
        queryKey: deploymentValuesKeys.value(value.id),
      });
    },
    [queryClient],
  );
}
