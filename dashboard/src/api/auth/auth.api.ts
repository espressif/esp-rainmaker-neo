/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  InitiateAuthCommand,
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
  ChangePasswordRequest,
  ChangePasswordResponse,
  ForgotPasswordRequest,
  ForgotPasswordResponse,
  ConfirmForgotPasswordRequest,
  ConfirmForgotPasswordResponse,
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

      const result = response.AuthenticationResult
      if (!result?.AccessToken) {
        throw new ApiError("Authentication failed", "AUTH_FAILED", 401)
      }

      return {
        access_token: result.AccessToken,
        refresh_token: result.RefreshToken ?? "",
        id_token: result.IdToken ?? "",
        token_type: result.TokenType ?? "Bearer",
      }
    } catch (error) {
      throw toApiError(error)
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
          PreviousPassword: data.old_password,
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
