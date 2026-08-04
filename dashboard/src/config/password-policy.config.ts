/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** A single rule the admin user pool enforces on new passwords. */
export type PasswordPolicyRule = {
  /** Stable identifier, used as the React key and in {@link evaluatePasswordPolicy} results. */
  id: string;
  /** Fully-qualified i18n key, so it resolves whatever namespace the caller's `t` is bound to. */
  i18nKey: string;
  /**
   * English fallback. Doubles as the zod validation message so the rule text a form
   * shows in a checklist and the text it shows in a field error are the same string.
   */
  fallback: string;
  /** Returns `true` when the candidate password satisfies this rule. */
  test: (value: string) => boolean;
};

/** Result of checking one rule against a candidate password. */
export type PasswordPolicyRuleState = {
  rule: PasswordPolicyRule;
  met: boolean;
};

/**
 * Admin Cognito user pool password policy, in the order it is presented to the user.
 *
 * Single source of truth: `newPasswordSchema` in
 * [auth.schemas.ts](../api/auth/auth.schemas.ts) builds its validation from this list, and
 * the requirements checklist renders from the same list — so what a form validates and what
 * it tells the user to type cannot drift apart. Changing the pool policy means editing this
 * array and adding one string per locale.
 */
export const PASSWORD_POLICY_RULES: readonly PasswordPolicyRule[] = [
  {
    id: "minLength",
    i18nKey: "common:passwordPolicy.minLength",
    fallback: "Password must be at least 8 characters",
    test: (value) => value.length >= 8,
  },
  {
    id: "uppercase",
    i18nKey: "common:passwordPolicy.uppercase",
    fallback: "Must contain an uppercase letter",
    test: (value) => /[A-Z]/.test(value),
  },
  {
    id: "lowercase",
    i18nKey: "common:passwordPolicy.lowercase",
    fallback: "Must contain a lowercase letter",
    test: (value) => /[a-z]/.test(value),
  },
  {
    id: "digit",
    i18nKey: "common:passwordPolicy.digit",
    fallback: "Must contain a digit",
    test: (value) => /[0-9]/.test(value),
  },
  {
    id: "specialCharacter",
    i18nKey: "common:passwordPolicy.specialCharacter",
    fallback: "Must contain a special character",
    test: (value) => /[^A-Za-z0-9]/.test(value),
  },
];

/**
 * Checks a candidate password against every rule, preserving policy order. Each
 * result carries its rule so callers can translate the label without re-pairing
 * the two lists by index.
 */
export function evaluatePasswordPolicy(
  value: string,
): PasswordPolicyRuleState[] {
  return PASSWORD_POLICY_RULES.map((rule) => ({
    rule,
    met: rule.test(value),
  }));
}
