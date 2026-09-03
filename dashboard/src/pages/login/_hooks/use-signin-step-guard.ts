/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { getLastLoginEmail } from "@/lib/auth";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { preserveSearch } from "../_utils/preserve-search";

/**
 * Where identification starts: the remembered-account screen when this browser
 * has signed in before, the email form otherwise. Every bounce and every Back
 * that leaves the flow resolves through this one rule.
 */
export function signinEntryRoute(): "/login" | "/login/email" {
  return getLastLoginEmail() ? "/login" : "/login/email";
}

/**
 * The route-guard rule: a step route that requires flow state it does not have
 * redirects (`replace`, search preserved) to the entry route. One rule covers
 * refresh, deep links, and expired sessions alike — `sessionExpired` merely
 * drops the session from the store and lets this guard do the redirecting.
 *
 * Returns whether the requirement is met, so the page can render nothing while
 * the redirect is in flight.
 */
export function useSigninStepGuard(requires: "username" | "session"): boolean {
  const navigate = useNavigate();
  const username = useSigninFlowStore((s) => s.username);
  const session = useSigninFlowStore((s) => s.session);

  const satisfied =
    requires === "session" ? Boolean(username && session) : Boolean(username);

  useEffect(() => {
    if (!satisfied) {
      void navigate({
        to: signinEntryRoute(),
        search: preserveSearch,
        replace: true,
      });
    }
  }, [satisfied, navigate]);

  return satisfied;
}
