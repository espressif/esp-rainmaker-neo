/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  InitiateAuthCommand,
  RespondToAuthChallengeCommand,
  GetUserAuthFactorsCommand,
  ChangePasswordCommand,
  ForgotPasswordCommand,
  ConfirmForgotPasswordCommand,
} from "@aws-sdk/client-cognito-identity-provider";
import { getCognitoClient } from "../../lib/auth/cognito-client";
import { getCognitoClientId } from "../../lib/config";
import { ApiError } from "../api.errors";
import type {
  SigninRequest,
  SigninResponse,
  StartSigninRequest,
  StartSigninResult,
  SelectOtpRequest,
  OtpChallenge,
  VerifyOtpRequest,
  UserAuthFactors,
  ChangePasswordRequest,
  ChangePasswordResponse,
  ForgotPasswordRequest,
  ForgotPasswordResponse,
  ConfirmForgotPasswordRequest,
  ConfirmForgotPasswordResponse,
  ChallengeKind,
} from "./auth.types";

/**
 * Normalize an AWS SDK / Cognito error into an ApiError so callers can keep
 * the existing typed error-handling style used across the API layer.
 */
function toApiError(error: unknown): ApiError {
  if (ApiError.isApiError(error)) {
    return error
  }
  if (error && typeof error === "object") {
    const err = error as {
      name?: string
      message?: string
      $metadata?: { httpStatusCode?: number }
    }
    return new ApiError(
      err.message || "An unexpected error occurred",
      err.name || "COGNITO_ERROR",
      err.$metadata?.httpStatusCode ?? 0,
    )
  }
  return new ApiError("An unexpected error occurred", "UNKNOWN_ERROR", 0)
}

/** The factors this app can drive; anything else Cognito offers is ignored. */
const SUPPORTED_CHALLENGES: readonly string[] = ["EMAIL_OTP", "PASSWORD"]

/** Cognito's error name when an app client does not permit the requested flow. */
const FLOW_NOT_PERMITTED = "InvalidParameterException"

function toAuthResponse(result: {
  AccessToken?: string
  RefreshToken?: string
  IdToken?: string
  TokenType?: string
} | undefined): SigninResponse {
  if (!result?.AccessToken) {
    throw new ApiError("Authentication failed", "AUTH_FAILED", 401)
  }
  return {
    access_token: result.AccessToken,
    refresh_token: result.RefreshToken ?? "",
    id_token: result.IdToken ?? "",
    token_type: result.TokenType ?? "Bearer",
  }
}

/**
 * A missing `Session` cannot be threaded into the next `RespondToAuthChallenge` call,
 * so this fails fast here rather than letting Cognito reject an empty session far
 * from where the real problem occurred.
 */
function requireSession(session: string | undefined): string {
  if (!session) {
    throw new ApiError("Malformed challenge response", "COGNITO_ERROR", 0)
  }
  return session
}

/**
 * Auth API functions
 *
 * Admins authenticate directly against the admin AWS Cognito user pool via the
 * AWS SDK (the ESP User `/v1/admin/auth/*` API has been removed).
 */
export const authApi = {
  /**
   * Authenticate an admin against Cognito with username + password.
   */
  signin: async (data: SigninRequest): Promise<SigninResponse> => {
    try {
      const response = await getCognitoClient().send(
        new InitiateAuthCommand({
          AuthFlow: "USER_PASSWORD_AUTH",
          ClientId: getCognitoClientId(),
          AuthParameters: {
            USERNAME: data.username,
            PASSWORD: data.password,
          },
        }),
      )

      return toAuthResponse(response.AuthenticationResult)
    } catch (error) {
      throw toApiError(error)
    }
  },

  /**
   * Begin choice-based sign-in. Cognito answers with the factors this address
   * actually has, so the page never prompts for a credential the admin lacks.
   */
  startSignin: async (data: StartSigninRequest): Promise<StartSigninResult> => {
    try {
      const response = await getCognitoClient().send(
        new InitiateAuthCommand({
          AuthFlow: "USER_AUTH",
          ClientId: getCognitoClientId(),
          AuthParameters: { USERNAME: data.username },
        }),
      )

      if (response.ChallengeName === "EMAIL_OTP") {
        return {
          kind: "otpSent",
          session: requireSession(response.Session),
          destination: response.ChallengeParameters?.CODE_DELIVERY_DESTINATION ?? "",
        }
      }

      const challenges = (response.AvailableChallenges ?? []).filter(
        (c): c is ChallengeKind => SUPPORTED_CHALLENGES.includes(c),
      )
      return { kind: "select", session: requireSession(response.Session), challenges }
    } catch (error) {
      // An app client without ALLOW_USER_AUTH rejects the flow outright. That is a
      // deployment that has not upgraded yet, not a failure the admin can act on, so
      // the caller falls back to password sign-in instead of surfacing an error.
      if ((error as { name?: string })?.name === FLOW_NOT_PERMITTED) {
        return { kind: "unavailable" }
      }
      throw toApiError(error)
    }
  },

  /**
   * Choose the email-OTP factor from the offered challenges. Cognito mails the code
   * and returns a rotated session.
   */
  selectOtp: async (data: SelectOtpRequest): Promise<OtpChallenge> => {
    try {
      const response = await getCognitoClient().send(
        new RespondToAuthChallengeCommand({
          ClientId: getCognitoClientId(),
          ChallengeName: "SELECT_CHALLENGE",
          Session: data.session,
          ChallengeResponses: { USERNAME: data.username, ANSWER: "EMAIL_OTP" },
        }),
      )

      return {
        session: requireSession(response.Session),
        destination: response.ChallengeParameters?.CODE_DELIVERY_DESTINATION ?? "",
      }
    } catch (error) {
      throw toApiError(error)
    }
  },

  /**
   * Answer an email-OTP challenge with the code from the admin's inbox.
   */
  verifyOtp: async (data: VerifyOtpRequest): Promise<SigninResponse> => {
    try {
      const response = await getCognitoClient().send(
        new RespondToAuthChallengeCommand({
          ClientId: getCognitoClientId(),
          ChallengeName: "EMAIL_OTP",
          Session: data.session,
          ChallengeResponses: {
            USERNAME: data.username,
            EMAIL_OTP_CODE: data.code,
          },
        }),
      )

      return toAuthResponse(response.AuthenticationResult)
    } catch (error) {
      throw toApiError(error)
    }
  },

  /**
   * Read the signed-in admin's configured first factors.
   *
   * An admin seeded after passwordless sign-in has no password, and Cognito accepts
   * `ChangePassword` without a previous password only for them — so the account
   * settings form has to know which case it is in. A failure is reported as
   * "has a password", which renders the pre-existing form unchanged.
   */
  getUserAuthFactors: async (accessToken: string): Promise<UserAuthFactors> => {
    try {
      const response = await getCognitoClient().send(
        new GetUserAuthFactorsCommand({ AccessToken: accessToken }),
      )
      return {
        hasPassword: (response.ConfiguredUserAuthFactors ?? []).includes("PASSWORD"),
      }
    } catch {
      return { hasPassword: true }
    }
  },

  /**
   * Change the current admin's password (requires a valid access token).
   */
  changePassword: async (data: ChangePasswordRequest): Promise<ChangePasswordResponse> => {
    try {
      await getCognitoClient().send(
        new ChangePasswordCommand({
          AccessToken: data.access_token,
          // Left off entirely for a passwordless admin: Cognito documents
          // PreviousPassword as omissible only in that case, and rejects an empty one.
          ...(data.old_password ? { PreviousPassword: data.old_password } : {}),
          ProposedPassword: data.new_password,
        }),
      )

      return {
        message: "Password changed successfully",
        success: true,
      }
    } catch (error) {
      throw toApiError(error)
    }
  },

  /**
   * Start a password reset: Cognito sends a confirmation code to the address
   * registered on the account.
   *
   * An app client with user-existence errors suppressed already answers
   * successfully for unknown addresses; swallowing `UserNotFoundException`
   * makes the two configurations behave identically, so a caller can never
   * tell a registered admin from an unregistered one.
   */
  forgotPassword: async (data: ForgotPasswordRequest): Promise<ForgotPasswordResponse> => {
    try {
      await getCognitoClient().send(
        new ForgotPasswordCommand({
          ClientId: getCognitoClientId(),
          Username: data.username,
        }),
      )
    } catch (error) {
      const name = (error as { name?: string } | null)?.name
      if (name !== "UserNotFoundException") {
        throw toApiError(error)
      }
    }

    return {
      message: "Password reset code requested",
      success: true,
    }
  },

  /**
   * Complete a password reset with the emailed confirmation code.
   */
  confirmForgotPassword: async (
    data: ConfirmForgotPasswordRequest,
  ): Promise<ConfirmForgotPasswordResponse> => {
    try {
      await getCognitoClient().send(
        new ConfirmForgotPasswordCommand({
          ClientId: getCognitoClientId(),
          Username: data.username,
          ConfirmationCode: data.code,
          Password: data.new_password,
        }),
      )

      return {
        message: "Password reset successfully",
        success: true,
      }
    } catch (error) {
      throw toApiError(error)
    }
  },
} as const;
