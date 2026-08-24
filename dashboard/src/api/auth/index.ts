/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Auth API module exports
 */

export type {
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
} from './auth.types'

export {
  getAuthSchemaMessages,
  type AuthSchemaMessages,
  getSigninRequestSchema,
  type SigninRequestSchema,
  getIdentifyRequestSchema,
  type IdentifyRequestSchema,
  getOtpRequestSchema,
  type OtpRequestSchema,
  getChangePasswordRequestSchema,
  type ChangePasswordRequestSchema,
  getForgotPasswordRequestSchema,
  type ForgotPasswordRequestSchema,
  getConfirmForgotPasswordRequestSchema,
  type ConfirmForgotPasswordRequestSchema,
} from './auth.schemas'

export { authApi } from './auth.api'

// Mutation hooks
export {
  useSignin,
  useChangePassword,
  useForgotPassword,
  useConfirmForgotPassword,
  useStartSignin,
  useSelectOtp,
  useVerifyOtp,
  useUserAuthFactors,
} from './auth.queries'
