/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { startSessionKeeper } from "@/lib/auth";
import { toAwsCredentials, userQueries } from "@/api";

/**
 * Keeps the signed-in session alive in the background for users who opted into
 * "keep me signed in". Inert for everyone else — without that opt-in no refresh token
 * is stored, so there is nothing to renew.
 *
 * Mount once, inside the authenticated shell. Failures are silent by design.
 */
export function useSessionKeeper(): void {
  const queryClient = useQueryClient();

  useEffect(
    () =>
      startSessionKeeper({
        // Going through the query cache keeps the renewed credentials in the same
        // entry `useGetUserCreds` reads, so the page never refetches behind us.
        // `staleTime: 0` overrides the factory's 30 minutes: a renewal must hit the
        // network, never hand back the credentials that are about to expire.
        refreshCredentials: async () =>
          toAwsCredentials(
            await queryClient.fetchQuery({ ...userQueries.creds(), staleTime: 0 }),
          ),
      }),
    [queryClient],
  );
}
