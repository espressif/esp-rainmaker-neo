/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useSelectOtp, useStartSignin, useVerifyOtp } from "@/api";
import { ApiError } from "@/api/api.errors";
import { OTP_SESSION_EXPIRED_MESSAGE } from "@/lib/auth";
import { useSigninFlowStore } from "@/stores/signin-flow.store";
import { offersOtp } from "../_utils/signin-flow";
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
 * Wires the pure sign-in reducer (via the cross-route store) to the Cognito
 * mutations, and moves the admin between step routes as outcomes come in. The
 * machine itself lives in `_utils/signin-flow.ts` and is unit-tested there;
 * mutations — and therefore their errors and pending flags — are local to the
 * page that called this hook, while the flow state itself lives in the store.
 *
 * A code is emailed only when the admin asks for one; choosing the password contacts Cognito not at all until the password is submitted.
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

  // Cognito rotates the auth session on every challenge response and rejects a spent one, so each send selects EMAIL_OTP against the latest session.
  const selectCode = useCallback(
    (session: string, username: string, onSent?: () => void) => {
      selectMutation.mutate(
        { username, session },
        {
          onSuccess: (challenge) => {
            useSigninFlowStore.getState().dispatch({
              type: "otpSent",
              session: challenge.session,
              destination: challenge.destination,
            });
            onSent?.();
          },
          onError: (error) => {
            if (isSessionRejected(error)) {
              expireSession();
            }
          },
        },
      );
    },
    [expireSession, selectMutation],
  );

  const startCode = useCallback(
    (username: string, onSent?: () => void) => {
      startMutation.mutate(
        { username },
        {
          onSuccess: (result) => {
            const { dispatch } = useSigninFlowStore.getState();
            // The app client predates ALLOW_USER_AUTH; the password form is the one path that works against either version of the stack.
            if (result.kind === "unavailable") {
              dispatch({ type: "userAuthUnavailable" });
              goTo("/login/password");
              return;
            }
            // Cognito mails the code itself when EMAIL_OTP is the account's only factor.
            if (result.kind === "otpSent") {
              dispatch({
                type: "otpSent",
                session: result.session,
                destination: result.destination,
              });
              onSent?.();
              return;
            }
            dispatch({
              type: "challenges",
              session: result.session,
              challenges: result.challenges,
            });
            if (offersOtp(result.challenges)) {
              selectCode(result.session, username, onSent);
              return;
            }
            goTo("/login/password");
          },
        },
      );
    },
    [goTo, selectCode, startMutation],
  );

  // A fresh attempt must not carry forward whatever the previous one left behind, including the expired-session notice.
  const startOver = useCallback(
    (username: string) => {
      const store = useSigninFlowStore.getState();
      store.setFlowMessage(null);
      startMutation.reset();
      selectMutation.reset();
      verifyMutation.reset();
      store.dispatch({ type: "identified", username });
    },
    [selectMutation, startMutation, verifyMutation],
  );

  const continueWithCode = useCallback(
    (username: string) => {
      startOver(username);
      startCode(username, () => goTo("/login/otp"));
    },
    [goTo, startCode, startOver],
  );

  // Nothing reaches Cognito here: asking it for the account's factors would mail a code to a passwordless admin.
  const continueWithPassword = useCallback(
    (username: string) => {
      startOver(username);
      useSigninFlowStore.getState().dispatch({ type: "usePassword" });
      goTo("/login/password");
    },
    [goTo, startOver],
  );

  const sendCode = useCallback(
    (onSent?: () => void) => {
      const { session, username, codeSent } = useSigninFlowStore.getState();
      if (codeSent && session) {
        selectCode(session, username, onSent);
        return;
      }
      startCode(username, onSent);
    },
    [selectCode, startCode],
  );

  const verify = useCallback(
    (code: string) => {
      // A fresh code submission must not carry forward an error left by an earlier step.
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

  return {
    continueWithCode,
    continueWithPassword,
    sendCode,
    verify,
    isSending: startMutation.isPending || selectMutation.isPending,
    isVerifying: verifyMutation.isPending,
    error:
      startMutation.error ??
      selectMutation.error ??
      verifyMutation.error ??
      null,
  };
}
