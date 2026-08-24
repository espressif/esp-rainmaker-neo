/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from 'i18next'
import { z } from 'zod'
import { PASSWORD_POLICY_RULES } from '@/config/password-policy.config'

/**
 * Validation copy for the auth forms. Built once per render from the caller's
 * `t` and handed to the schema factories below, so the same schemas serve the
 * login, forgot-password and change-password forms without any of them owning
 * an English string. Keys live under `common` because three different route
 * namespaces render these messages.
 */
export interface AuthSchemaMessages {
  emailRequired: string
  emailInvalid: string
  passwordRequired: string
  currentPasswordRequired: string
  confirmPasswordRequired: string
  passwordsDoNotMatch: string
  codeRequired: string
  codeInvalid: string
  /** One message per password-policy rule, keyed by rule id. */
  passwordPolicy: Record<string, string>
}

export function getAuthSchemaMessages(t: TFunction): AuthSchemaMessages {
  return {
    emailRequired: t('common:authErrors.emailRequired', 'Email is required'),
    emailInvalid: t('common:authErrors.emailInvalid', 'Please enter a valid email address'),
    passwordRequired: t('common:authErrors.passwordRequired', 'Password is required'),
    currentPasswordRequired: t(
      'common:authErrors.currentPasswordRequired',
      'Current password is required',
    ),
    confirmPasswordRequired: t(
      'common:authErrors.confirmPasswordRequired',
      'Please confirm your new password',
    ),
    passwordsDoNotMatch: t('common:authErrors.passwordsDoNotMatch', 'Passwords do not match'),
    codeRequired: t('common:authErrors.codeRequired', 'Confirmation code is required'),
    codeInvalid: t('common:authErrors.codeInvalid', 'Enter the 6-digit code you received'),
    passwordPolicy: Object.fromEntries(
      PASSWORD_POLICY_RULES.map((rule) => [rule.id, t(rule.i18nKey, rule.fallback)]),
    ),
  }
}

/**
 * Admin user pool password policy. Shared by every flow that sets a password so
 * the rules the forms enforce cannot drift apart from each other or from Cognito.
 *
 * The rules themselves live in `password-policy.config.ts` so the requirements
 * checklist the change-password form renders is generated from the same list that
 * validates here. Every failing rule raises its own issue, matching the per-rule
 * messages this schema produced when the checks were chained `.regex()` calls.
 */
const newPasswordSchema = (messages: AuthSchemaMessages) =>
  z.string().superRefine((value, ctx) => {
    for (const rule of PASSWORD_POLICY_RULES) {
      if (!rule.test(value)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.passwordPolicy[rule.id] ?? rule.fallback,
        })
      }
    }
  })

const confirmPasswordSchema = (messages: AuthSchemaMessages) =>
  z.string().min(1, messages.confirmPasswordRequired)

const passwordsMatch = {
  check: (data: { new_password: string; confirm_password: string }) =>
    data.new_password === data.confirm_password,
  options: (messages: AuthSchemaMessages) => ({
    message: messages.passwordsDoNotMatch,
    path: ['confirm_password'],
  }),
}

/**
 * Zod schema for signin request validation
 */
export const getSigninRequestSchema = (messages: AuthSchemaMessages) =>
  z.object({
    username: z.string().min(1, messages.emailRequired).email(messages.emailInvalid),
    password: z.string().min(1, messages.passwordRequired),
  })

export type SigninRequestSchema = z.infer<ReturnType<typeof getSigninRequestSchema>>

/**
 * Zod schema for the first login step, which collects only an address
 */
export const getIdentifyRequestSchema = (messages: AuthSchemaMessages) =>
  z.object({
    username: z.string().min(1, messages.emailRequired).email(messages.emailInvalid),
  })

export type IdentifyRequestSchema = z.infer<ReturnType<typeof getIdentifyRequestSchema>>

/**
 * Zod schema for the emailed one-time code
 */
export const getOtpRequestSchema = (messages: AuthSchemaMessages) =>
  z.object({
    code: z.string().min(1, messages.codeRequired),
  })

export type OtpRequestSchema = z.infer<ReturnType<typeof getOtpRequestSchema>>

/**
 * Zod schema for change password request validation.
 *
 * `requireCurrentPassword` is false for an admin with no password yet (the
 * "set a first password" form has no current-password field to render or
 * validate). The field itself stays a plain required-shape `z.string()` in both
 * cases — branching between `.min(1, …)` and `.optional()` would give the field
 * two different static types depending on a runtime boolean, which `zodResolver`
 * cannot reconcile with a single `useForm<ChangePasswordRequestSchema>()` call.
 * The emptiness check instead runs as a `superRefine`, so it can be skipped
 * without changing the field's type.
 */
export const getChangePasswordRequestSchema = (
  messages: AuthSchemaMessages,
  { requireCurrentPassword = true }: { requireCurrentPassword?: boolean } = {},
) =>
  z
    .object({
      old_password: z.string(),
      new_password: newPasswordSchema(messages),
      confirm_password: confirmPasswordSchema(messages),
    })
    .superRefine((data, ctx) => {
      if (requireCurrentPassword && data.old_password.length === 0) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: messages.currentPasswordRequired,
          path: ['old_password'],
        })
      }
    })
    .refine(passwordsMatch.check, passwordsMatch.options(messages))

export type ChangePasswordRequestSchema = z.infer<
  ReturnType<typeof getChangePasswordRequestSchema>
>

/**
 * Zod schema for requesting a password reset code (step 1 of forgot password)
 */
export const getForgotPasswordRequestSchema = (messages: AuthSchemaMessages) =>
  z.object({
    username: z.string().min(1, messages.emailRequired).email(messages.emailInvalid),
  })

export type ForgotPasswordRequestSchema = z.infer<
  ReturnType<typeof getForgotPasswordRequestSchema>
>

/**
 * Zod schema for confirming a password reset (step 2 of forgot password).
 * Cognito always mails a six-digit confirmation code.
 */
export const getConfirmForgotPasswordRequestSchema = (messages: AuthSchemaMessages) =>
  z
    .object({
      code: z.string().min(1, messages.codeRequired).regex(/^\d{6}$/, messages.codeInvalid),
      new_password: newPasswordSchema(messages),
      confirm_password: confirmPasswordSchema(messages),
    })
    .refine(passwordsMatch.check, passwordsMatch.options(messages))

export type ConfirmForgotPasswordRequestSchema = z.infer<
  ReturnType<typeof getConfirmForgotPasswordRequestSchema>
>
