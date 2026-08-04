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
  ChangePasswordRequest,
  ChangePasswordResponse,
  ForgotPasswordRequest,
  ForgotPasswordResponse,
  ConfirmForgotPasswordRequest,
  ConfirmForgotPasswordResponse,
} from './auth.types'

export {
  getAuthSchemaMessages,
  type AuthSchemaMessages,
  getSigninRequestSchema,
  type SigninRequestSchema,
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
} from './auth.queries'
