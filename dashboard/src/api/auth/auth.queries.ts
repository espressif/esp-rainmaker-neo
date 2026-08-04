/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMutation } from '@tanstack/react-query'
import { authApi } from './auth.api'
import type {
  SigninRequest,
  SigninResponse,
  ChangePasswordRequest,
  ChangePasswordResponse,
  ForgotPasswordRequest,
  ForgotPasswordResponse,
  ConfirmForgotPasswordRequest,
  ConfirmForgotPasswordResponse,
} from './auth.types'

/**
 * Hook for authenticating a user via signin
 */
export function useSignin() {
  return useMutation<SigninResponse, Error, SigninRequest>({
    mutationFn: authApi.signin,
  })
}

/**
 * Hook for changing the user's password
 */
export function useChangePassword() {
  return useMutation<ChangePasswordResponse, Error, ChangePasswordRequest>({
    mutationFn: authApi.changePassword,
  })
}

/**
 * Hook for requesting a password reset code
 */
export function useForgotPassword() {
  return useMutation<ForgotPasswordResponse, Error, ForgotPasswordRequest>({
    mutationFn: authApi.forgotPassword,
  })
}

/**
 * Hook for completing a password reset with the emailed code
 */
export function useConfirmForgotPassword() {
  return useMutation<ConfirmForgotPasswordResponse, Error, ConfirmForgotPasswordRequest>({
    mutationFn: authApi.confirmForgotPassword,
  })
}
