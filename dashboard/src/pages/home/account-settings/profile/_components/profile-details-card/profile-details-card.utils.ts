/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type {
  DynamicListEntry,
  DynamicListMetaEntry,
} from "@espressif/dashboard-ui-components/components";
import type { IdTokenClaims } from "@/lib/auth";

/**
 * Claims remapped to display keys, in display order. `cognito:username`
 * becomes `username` so DynamicList meta keys stay plain identifiers.
 */
const FIELD_ORDER = [
  "username",
  "email",
  "email_verified",
  "sub",
  "auth_time",
] as const;

type ProfileFieldKey = (typeof FIELD_ORDER)[number];

/**
 * Builds the DynamicList rows from the decoded claims, skipping absent fields
 * so the card never shows blank rows. `email_verified: false` is kept — it
 * renders as a cross icon via `useIconsForBoolean`.
 *
 * Email-sign-in pools auto-generate `cognito:username` as the same UUID as
 * `sub`, so the username row only renders when it's a real, distinct name.
 */
export function buildProfileItems(claims: IdTokenClaims): DynamicListEntry[] {
  const cognitoUsername = claims["cognito:username"];
  const isDistinctUsername =
    cognitoUsername !== claims.sub && cognitoUsername !== claims.email;

  const fields: Record<ProfileFieldKey, unknown> = {
    username: isDistinctUsername ? cognitoUsername : undefined,
    email: claims.email,
    email_verified: claims.email_verified,
    sub: claims.sub,
    auth_time: claims.auth_time,
  };

  return FIELD_ORDER.reduce<DynamicListEntry[]>((entries, key) => {
    const value = fields[key];
    if (value != null && value !== "") {
      entries.push({ key, value });
    }
    return entries;
  }, []);
}

/**
 * Per-key rendering config for the DynamicList. `auth_time` needs an explicit
 * timestamp type (the key lacks "timestamp"/"_ts", so auto-detection won't
 * kick in); `sub` renders mono since it's a copyable identifier.
 */
export function buildProfileMeta(
  t: TFunction,
): Record<string, DynamicListMetaEntry> {
  return {
    username: { label: t("profile.details.labels.username", "Username") },
    email: { label: t("profile.details.labels.email", "Email") },
    email_verified: {
      label: t("profile.details.labels.emailVerified", "Email verified"),
    },
    sub: { type: "mono", label: t("profile.details.labels.userId", "User ID") },
    auth_time: {
      type: "timestamp",
      label: t("profile.details.labels.lastSignIn", "Last sign-in"),
    },
  };
}
