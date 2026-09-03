/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { resetLogoutFlag, type SigninResponse } from "@/api";
import {
  consumeRedirectPath,
  storeAuthTokens,
  storeKeepSignedIn,
  storeLastLoginEmail,
} from "@/lib/auth";
import { useUserStore } from "@/stores/user.store";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { parseLoginSearch } from "../_schema/login-search.schema";

/**
 * The single completion point for both the OTP and the password path: persist the
 * session, remember the account for the next visit, and leave the flow.
 */
export function useCompleteSignin() {
  const navigate = useNavigate();
  const location = useLocation();
  const setLoggedInUserName = useUserStore((s) => s.setLoggedInUserName);
  const loginSearch = useMemo(
    () => parseLoginSearch(location.search),
    [location.search],
  );

  return useCallback(
    (username: string, response: SigninResponse) => {
      const { keepSignedIn } = useSigninFlowStore.getState();

      // The refresh token is persisted only on explicit opt-in: it is the one
      // credential that outlives the browser session.
      storeAuthTokens({
        accessToken: response.access_token,
        idToken: response.id_token,
        refreshToken: keepSignedIn ? response.refresh_token : undefined,
      });
      storeKeepSignedIn(keepSignedIn);
      // The one persisted trace of the flow itself: it feeds the
      // remembered-account screen on the next visit.
      storeLastLoginEmail(username);
      setLoggedInUserName(username);
      resetLogoutFlag();

      // `?redirect=` wins: it is the destination the dead session handed over,
      // and it survives the hard navigation that `logout()` performs.
      const redirectPath = loginSearch.redirect ?? consumeRedirectPath();
      // The store is cleared only after the step page has unmounted — clearing
      // first would trip that page's guard, which would race this navigation
      // with its own bounce to the entry screen.
      void navigate({ to: redirectPath || "/home" }).then(() =>
        useSigninFlowStore.getState().reset(),
      );
    },
    [loginSearch.redirect, navigate, setLoggedInUserName],
  );
}
