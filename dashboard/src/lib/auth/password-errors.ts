/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Imported from the module rather than the `@/api` barrel: that barrel reaches
// back into `@/lib/auth`, and the cycle splits this module across chunks.
import { ApiError } from "@/api/api.errors";

/**
 * A fully-qualified i18next key plus its English fallback, so callers keep the
 * `t(key, fallback)` style used across the pages. Keys are namespaced against
 * `common` because every password flow resolves them from its own page namespace.
 */
export type LocalizedMessage = {
  key: string;
  fallback: string;
};

const GENERIC: LocalizedMessage = {
  key: "common:resetPasswordErrors.generic",
  fallback: "Something went wrong. Please try again later.",
};

const RATE_LIMITED: LocalizedMessage = {
  key: "common:resetPasswordErrors.rateLimited",
  fallback:
    "Too many attempts. Only about 5 reset requests per hour are allowed — wait a while before trying again.",
};

const INVALID_CODE: LocalizedMessage = {
  key: "common:resetPasswordErrors.codeMismatch",
  fallback: "That code is not correct. Check the code and try again.",
};

const CANNOT_RESET: LocalizedMessage = {
  key: "common:resetPasswordErrors.cannotReset",
  fallback:
    "This address cannot be reset automatically. Contact your administrator.",
};

const CODE_EXPIRED: LocalizedMessage = {
  key: "common:resetPasswordErrors.codeExpired",
  fallback: "That code has expired. Request a new one.",
};

const INVALID_PASSWORD: LocalizedMessage = {
  key: "common:resetPasswordErrors.invalidPassword",
  fallback:
    "That password does not meet the password policy. Choose a different one.",
};

const INCORRECT_CURRENT_PASSWORD: LocalizedMessage = {
  key: "common:passwordErrors.incorrectCurrentPassword",
  fallback: "Your current password is not correct. Try again.",
};

const SESSION_EXPIRED: LocalizedMessage = {
  key: "common:passwordErrors.sessionExpired",
  fallback: "Your session has expired. Sign in again to change your password.",
};

const INVALID_REQUEST: LocalizedMessage = {
  key: "common:passwordErrors.invalidRequest",
  fallback: "That request could not be processed. Check your input and retry.",
};

/**
 * Cognito exception name -> message for the "send me a code" step. Missing
 * accounts are absent on purpose: the API layer reports success for them.
 */
const REQUEST_CODE_ERRORS: Record<string, LocalizedMessage> = {
  LimitExceededException: RATE_LIMITED,
  TooManyRequestsException: RATE_LIMITED,
  // Raised when the account has no verified email or phone number, and for
  // disabled accounts. Worded so it never confirms that the account exists.
  InvalidParameterException: CANNOT_RESET,
  NotAuthorizedException: CANNOT_RESET,
};

/**
 * Cognito exception name -> message for the "set a new password" step.
 * `UserNotFoundException` maps onto the wrong-code message so a guessed
 * address still cannot be distinguished from a wrong code.
 */
const CONFIRM_RESET_ERRORS: Record<string, LocalizedMessage> = {
  CodeMismatchException: INVALID_CODE,
  UserNotFoundException: INVALID_CODE,
  ExpiredCodeException: CODE_EXPIRED,
  // Cognito also uses this once a code has already been consumed.
  NotAuthorizedException: CODE_EXPIRED,
  InvalidPasswordException: INVALID_PASSWORD,
  LimitExceededException: RATE_LIMITED,
  TooManyRequestsException: RATE_LIMITED,
};

/**
 * Cognito exception name -> message for the signed-in "change my password" flow.
 *
 * `NotAuthorizedException` is raised both for a wrong current password and for a
 * revoked access token. The wrong-password case is by far the more common one for an
 * admin who is already signed in, so it wins the message; a genuinely dead session
 * shows up as a missing token before the request is ever sent.
 */
const CHANGE_PASSWORD_ERRORS: Record<string, LocalizedMessage> = {
  NotAuthorizedException: INCORRECT_CURRENT_PASSWORD,
  InvalidPasswordException: INVALID_PASSWORD,
  LimitExceededException: RATE_LIMITED,
  TooManyRequestsException: RATE_LIMITED,
  InvalidParameterException: INVALID_REQUEST,
  PasswordResetRequiredException: SESSION_EXPIRED,
  UserNotFoundException: SESSION_EXPIRED,
};

/** Message shown when there is no usable access token to change a password with. */
export const SESSION_EXPIRED_MESSAGE = SESSION_EXPIRED;

/**
 * Whether a change-password failure was caused by the wrong current password, so a
 * form can attach the message to that field instead of only to a summary alert.
 */
export function isIncorrectCurrentPasswordError(error: unknown): boolean {
  return (
    ApiError.isApiError(error) && error.code === "NotAuthorizedException"
  );
}

/**
 * Raw SDK exception text is never surfaced — unmapped failures fall back to a
 * generic message.
 */
function resolve(
  error: unknown,
  table: Record<string, LocalizedMessage>,
): LocalizedMessage | null {
  if (!error) {
    return null;
  }
  if (!ApiError.isApiError(error)) {
    return GENERIC;
  }
  return table[error.code] ?? GENERIC;
}

/** Message for a failed "mail me a confirmation code" request. */
export function requestCodeErrorMessage(
  error: unknown,
): LocalizedMessage | null {
  return resolve(error, REQUEST_CODE_ERRORS);
}

/** Message for a failed "exchange the code for a new password" request. */
export function confirmResetErrorMessage(
  error: unknown,
): LocalizedMessage | null {
  return resolve(error, CONFIRM_RESET_ERRORS);
}

/** Message for a failed "change my own password" request. */
export function changePasswordErrorMessage(
  error: unknown,
): LocalizedMessage | null {
  return resolve(error, CHANGE_PASSWORD_ERRORS);
}

const OTP_CODE_MISMATCH: LocalizedMessage = {
  key: "common:otpErrors.codeMismatch",
  fallback: "That code is not correct. Check the code and try again.",
};

const OTP_CODE_EXPIRED: LocalizedMessage = {
  key: "common:otpErrors.codeExpired",
  fallback: "That code has expired. Request a new one.",
};

const OTP_SESSION_EXPIRED: LocalizedMessage = {
  key: "common:otpErrors.sessionExpired",
  fallback: "That sign-in attempt timed out. Enter your email to start again.",
};

const OTP_RATE_LIMITED: LocalizedMessage = {
  key: "common:otpErrors.rateLimited",
  fallback: "Too many attempts. Wait a while before requesting another code.",
};

/**
 * Cognito exception name -> message for choice-based sign-in.
 *
 * `NotAuthorizedException` here means the short-lived auth session is spent, not a
 * bad credential: the code itself fails as `CodeMismatchException`.
 */
const OTP_ERRORS: Record<string, LocalizedMessage> = {
  CodeMismatchException: OTP_CODE_MISMATCH,
  ExpiredCodeException: OTP_CODE_EXPIRED,
  NotAuthorizedException: OTP_SESSION_EXPIRED,
  TooManyFailedAttemptsException: OTP_RATE_LIMITED,
  TooManyRequestsException: OTP_RATE_LIMITED,
};

/** Message for a failure during choice-based sign-in. */
export function otpErrorMessage(error: unknown): LocalizedMessage | null {
  return resolve(error, OTP_ERRORS);
}
