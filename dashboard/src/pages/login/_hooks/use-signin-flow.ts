/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useSelectOtp, useStartSignin, useVerifyOtp } from "@/api";
import { ApiError } from "@/api/api.errors";
import { OTP_SESSION_EXPIRED_MESSAGE, otpErrorMessage } from "@/lib/auth";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { shouldAutoSendOtp } from "../_utils/signin-flow";
import { preserveSearch } from "../_utils/preserve-search";
import { useCompleteSignin } from "./use-complete-signin";

/** Cognito's error name when an auth session is no longer usable. */
const SESSION_REJECTED = "NotAuthorizedException";

/**
 * The API layer normalises every Cognito failure into an `ApiError`, whose `name` is
 * always "ApiError" — the originating exception name survives on `code`.
 */
const isSessionRejected = (error: unknown) =>
  ApiError.isApiError(error) && error.code === SESSION_REJECTED;

/**
 * Cognito's names for "no more codes for now": the per-user cap on emailed codes
 * (`LimitExceededException`) and general request throttling. The auth session
 * itself is still valid — only the send was refused.
 */
const OTP_SEND_THROTTLED = new Set([
  "LimitExceededException",
  "TooManyRequestsException",
]);

const isSendThrottled = (error: unknown) =>
  ApiError.isApiError(error) && OTP_SEND_THROTTLED.has(error.code);

/**
 * Wires the pure sign-in reducer (via the cross-route store) to the Cognito
 * mutations, and moves the admin between step routes as outcomes come in. The
 * machine itself lives in `_utils/signin-flow.ts` and is unit-tested there;
 * mutations — and therefore their errors and pending flags — are local to the
 * page that called this hook, while the flow state itself lives in the store.
 *
 * Navigation only ever happens on mutation success (the OTP-send rule): the
 * admin lands on `/login/otp` with a code already mailed, never with one merely
 * hoped for.
 */
export function useSigninFlow() {
  const navigate = useNavigate();
  const completeSignin = useCompleteSignin();
  const startMutation = useStartSignin();
  const selectMutation = useSelectOtp();
  const verifyMutation = useVerifyOtp();

  const goTo = useCallback(
    (to: "/login/email" | "/login/otp" | "/login/password") => {
      void navigate({ to, search: preserveSearch });
    },
    [navigate],
  );

  // The session is spent: drop it (the step guard then bounces to the entry
  // route) and leave the message in the store so it survives that redirect.
  const expireSession = useCallback(() => {
    const store = useSigninFlowStore.getState();
    store.setFlowMessage(OTP_SESSION_EXPIRED_MESSAGE);
    store.dispatch({ type: "sessionExpired" });
  }, []);

  // Declared ahead of `identify`, which calls it directly: `InitiateAuth` only ever
  // answers with the menu, never with a mailed code, so a single-factor EMAIL_OTP
  // admin still needs this SELECT_CHALLENGE round-trip to get anything sent.
  const sendCode = useCallback(
    (
      session: string,
      username: string,
      options: { navigateToOtp: boolean; onSent?: () => void },
    ) => {
      selectMutation.mutate(
        { username, session },
        {
          onSuccess: (challenge) => {
            useSigninFlowStore.getState().dispatch({
              type: "otpSent",
              session: challenge.session,
              destination: challenge.destination,
            });
            options.onSent?.();
            if (options.navigateToOtp) {
              goTo("/login/otp");
            }
          },
          onError: (error) => {
            if (isSessionRejected(error)) {
              expireSession();
              return;
            }
            // The code path is throttled but the account has a password, so route
            // there instead of stranding the admin behind the rate limit. Only on
            // the way in (`navigateToOtp`): a throttled *resend* must stay on the
            // OTP screen — a code from an earlier send may still be usable.
            const store = useSigninFlowStore.getState();
            if (
              options.navigateToOtp &&
              isSendThrottled(error) &&
              store.challenges.includes("PASSWORD")
            ) {
              store.setFlowMessage(otpErrorMessage(error));
              goTo("/login/password");
            }
          },
        },
      );
    },
    [expireSession, goTo, selectMutation],
  );

  const identify = useCallback(
    (username: string) => {
      const store = useSigninFlowStore.getState();
      // A fresh attempt must not carry forward whatever the previous one left
      // behind — including the expired-session notice, deliberately kept until
      // the admin acts on it. This press is that act.
      store.setFlowMessage(null);
      selectMutation.reset();
      verifyMutation.reset();
      store.dispatch({ type: "identified", username });
      startMutation.mutate(
        { username },
        {
          onSuccess: (result) => {
            const { dispatch } = useSigninFlowStore.getState();
            // The app client predates ALLOW_USER_AUTH; the password form is the
            // one path that works against either version of the stack.
            if (result.kind === "unavailable") {
              dispatch({ type: "userAuthUnavailable" });
              goTo("/login/password");
              return;
            }
            if (result.kind === "otpSent") {
              dispatch({
                type: "otpSent",
                session: result.session,
                destination: result.destination,
              });
              goTo("/login/otp");
              return;
            }
            dispatch({
              type: "challenges",
              session: result.session,
              challenges: result.challenges,
            });
            // The code is the default way in, so it is requested without asking:
            // the admin lands on the code screen with one already mailed, and
            // reaches the password form only by opting out from there. An admin
            // without the email factor skips straight to the password screen.
            if (shouldAutoSendOtp(result.challenges)) {
              sendCode(result.session, username, { navigateToOtp: true });
            } else {
              goTo("/login/password");
            }
          },
        },
      );
    },
    [goTo, selectMutation, sendCode, startMutation, verifyMutation],
  );

  // Cognito rotates the auth session on every challenge response and rejects a spent
  // one, so this re-selects EMAIL_OTP against the session the last send returned —
  // not the one InitiateAuth issued, which is already consumed by then.
  const resend = useCallback(
    (onSent?: () => void) => {
      const { session, username } = useSigninFlowStore.getState();
      if (session) {
        sendCode(session, username, { navigateToOtp: false, onSent });
      }
    },
    [sendCode],
  );

  const verify = useCallback(
    (code: string) => {
      // Same reasoning as `identify`: a fresh code submission must not carry
      // forward an error left by an earlier step.
      startMutation.reset();
      selectMutation.reset();
      const { session, username } = useSigninFlowStore.getState();
      if (!session) {
        expireSession();
        return;
      }
      verifyMutation.mutate(
        { username, session, code },
        {
          onSuccess: (response) => completeSignin(username, response),
          onError: (error) => {
            if (isSessionRejected(error)) {
              expireSession();
            }
          },
        },
      );
    },
    [
      completeSignin,
      expireSession,
      selectMutation,
      startMutation,
      verifyMutation,
    ],
  );

  // Route changes double as "leave the errors behind": mutations are page-local
  // now, so whatever failed on the screen being left unmounts with it.
  const choosePassword = useCallback(() => {
    useSigninFlowStore.getState().dispatch({ type: "usePassword" });
    goTo("/login/password");
  }, [goTo]);

  const chooseOtp = useCallback(() => {
    useSigninFlowStore.getState().dispatch({ type: "useOtp" });
    goTo("/login/otp");
  }, [goTo]);

  return {
    identify,
    resend,
    verify,
    choosePassword,
    chooseOtp,
    isStarting: startMutation.isPending,
    isSending: selectMutation.isPending,
    isVerifying: verifyMutation.isPending,
    error:
      startMutation.error ?? selectMutation.error ?? verifyMutation.error ?? null,
  };
}
