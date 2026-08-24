/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useReducer } from "react";
import { useSelectOtp, useStartSignin, useVerifyOtp } from "@/api";
import type { SigninResponse } from "@/api";
import { ApiError } from "@/api/api.errors";
import {
  INITIAL_SIGNIN_STATE,
  shouldAutoSendOtp,
  signinReducer,
} from "../_utils/signin-flow";

/** Cognito's error name when an auth session is no longer usable. */
const SESSION_REJECTED = "NotAuthorizedException";

/**
 * The API layer normalises every Cognito failure into an `ApiError`, whose `name` is
 * always "ApiError" — the originating exception name survives on `code`.
 */
const isSessionRejected = (error: unknown) =>
  ApiError.isApiError(error) && error.code === SESSION_REJECTED;

interface UseSigninFlowOptions {
  onAuthenticated: (username: string, response: SigninResponse) => void;
}

/**
 * Wires the pure sign-in reducer to the Cognito mutations. The machine itself lives
 * in `_utils/signin-flow.ts` and is unit-tested there; this hook only performs the
 * I/O and translates outcomes into events.
 */
export function useSigninFlow({ onAuthenticated }: UseSigninFlowOptions) {
  const [state, dispatch] = useReducer(signinReducer, INITIAL_SIGNIN_STATE);
  const startMutation = useStartSignin();
  const selectMutation = useSelectOtp();
  const verifyMutation = useVerifyOtp();

  // Declared ahead of `identify`, which calls it directly: `InitiateAuth` only ever
  // answers with the menu, never with a mailed code, so a single-factor EMAIL_OTP
  // admin still needs this SELECT_CHALLENGE round-trip to get anything sent.
  const sendCode = useCallback(
    (session: string, username: string) => {
      selectMutation.mutate(
        { username, session },
        {
          onSuccess: (challenge) =>
            dispatch({
              type: "otpSent",
              session: challenge.session,
              destination: challenge.destination,
            }),
          onError: (error) => {
            if (isSessionRejected(error)) {
              dispatch({ type: "sessionExpired" });
            }
          },
        },
      );
    },
    [selectMutation],
  );

  const identify = useCallback(
    (username: string) => {
      // A fresh attempt must not carry forward whatever the step being left behind
      // produced — including a `sessionExpired` message, deliberately left set so it
      // stays legible on this very screen. `startMutation` is left alone: it is about
      // to run again immediately below, which clears its own prior error the normal way.
      selectMutation.reset();
      verifyMutation.reset();
      dispatch({ type: "identified", username });
      startMutation.mutate(
        { username },
        {
          onSuccess: (result) => {
            if (result.kind === "unavailable") {
              dispatch({ type: "userAuthUnavailable" });
              return;
            }
            if (result.kind === "otpSent") {
              dispatch({
                type: "otpSent",
                session: result.session,
                destination: result.destination,
              });
              return;
            }
            dispatch({
              type: "challenges",
              session: result.session,
              challenges: result.challenges,
            });
            // The code is the default way in, so it is requested without asking:
            // the admin lands on the code screen with one already mailed, and
            // reaches the password form only by opting out from there.
            if (shouldAutoSendOtp(result.challenges)) {
              sendCode(result.session, username);
            }
          },
        },
      );
    },
    [selectMutation, sendCode, startMutation, verifyMutation],
  );

  // Cognito rotates the auth session on every challenge response and rejects a spent
  // one, so this re-selects EMAIL_OTP against the session the last send returned —
  // not the one InitiateAuth issued, which is already consumed by then.
  const resend = useCallback(() => {
    if (state.session) {
      sendCode(state.session, state.username);
    }
  }, [sendCode, state.session, state.username]);

  // Both are explicit "start over" actions, so any stale error from the step being
  // left behind should not follow the admin onto the next screen. `identify` and
  // `verify` apply the same idea to their own new-attempt case (see there).
  // `sessionExpired` itself deliberately skips this reset: that message is meant to
  // survive the reducer's own return to `identify`, until the admin acts on it.
  const choosePassword = useCallback(() => {
    startMutation.reset();
    selectMutation.reset();
    verifyMutation.reset();
    dispatch({ type: "usePassword" });
  }, [startMutation, selectMutation, verifyMutation]);

  const restart = useCallback(() => {
    startMutation.reset();
    selectMutation.reset();
    verifyMutation.reset();
    dispatch({ type: "restart" });
  }, [startMutation, selectMutation, verifyMutation]);

  const verify = useCallback(
    (code: string) => {
      // Same reasoning as `identify`: a fresh code submission must not carry forward
      // an error left by an earlier step. `verifyMutation` is left alone — it is
      // about to run (or, on the early return below, was never the source anyway).
      startMutation.reset();
      selectMutation.reset();
      if (!state.session) {
        dispatch({ type: "sessionExpired" });
        return;
      }
      verifyMutation.mutate(
        { username: state.username, session: state.session, code },
        {
          onSuccess: (response) => onAuthenticated(state.username, response),
          onError: (error) => {
            if (isSessionRejected(error)) {
              dispatch({ type: "sessionExpired" });
            }
          },
        },
      );
    },
    [
      onAuthenticated,
      selectMutation,
      startMutation,
      state.session,
      state.username,
      verifyMutation,
    ],
  );

  return {
    state,
    identify,
    choosePassword,
    resend,
    restart,
    verify,
    isStarting: startMutation.isPending,
    isSending: selectMutation.isPending,
    isVerifying: verifyMutation.isPending,
    error:
      startMutation.error ?? selectMutation.error ?? verifyMutation.error ?? null,
  };
}
