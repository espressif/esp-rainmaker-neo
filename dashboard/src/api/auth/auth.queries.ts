/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMutation, useQuery } from '@tanstack/react-query'
import { authApi } from './auth.api'
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

/**
 * Hook for starting choice-based sign-in
 */
export function useStartSignin() {
  return useMutation<StartSigninResult, Error, StartSigninRequest>({
    mutationFn: authApi.startSignin,
  })
}

/**
 * Hook for choosing the email-OTP factor
 */
export function useSelectOtp() {
  return useMutation<OtpChallenge, Error, SelectOtpRequest>({
    mutationFn: authApi.selectOtp,
  })
}

/**
 * Hook for answering an email-OTP challenge
 */
export function useVerifyOtp() {
  return useMutation<SigninResponse, Error, VerifyOtpRequest>({
    mutationFn: authApi.verifyOtp,
  })
}

/**
 * Hook for reading the signed-in admin's configured first factors.
 *
 * Disabled without an access token so it never fires on a signed-out render.
 * `staleTime: Infinity` because the answer only changes when this admin adds a
 * password, and `useChangePasswordForm` invalidates `['auth', 'user-auth-factors']`
 * on that path, so this hook itself never needs to poll or refetch on an interval.
 */
export function useUserAuthFactors(accessToken: string | null) {
  return useQuery<UserAuthFactors>({
    queryKey: ['auth', 'user-auth-factors'],
    queryFn: () => authApi.getUserAuthFactors(accessToken ?? ''),
    enabled: Boolean(accessToken),
    staleTime: Infinity,
  })
}
