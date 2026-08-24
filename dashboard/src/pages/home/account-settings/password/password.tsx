/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import { ACCOUNT_SETTINGS_TABS_BY_ID } from "@/config/account-settings.config";
import { useUserAuthFactors } from "@/api";
import { getAccessToken } from "@/lib/auth";
import { ChangePasswordMainContent } from "./_components/change-password-main-content";
import { passwordModeFor } from "./_utils/password-factor";

const PASSWORD_TAB = ACCOUNT_SETTINGS_TABS_BY_ID.password;

/**
 * Change password tab. Holds only whether the change has already succeeded — the form
 * owns its own validation, mutation and failure state.
 *
 * Also reads this admin's factors to pick the card's own heading: an admin with no
 * password yet is setting one for the first time, not changing an existing one, and
 * the card should say so rather than always reading "Change password".
 *
 * The section shell (`account-settings.tsx`) supplies `PageContainer` and the page
 * heading, so this tab renders a bare card.
 */
export default function Password() {
  const { t } = useTranslation("account-settings");
  const [isPasswordChanged, setPasswordChanged] = useState(false);
  const { data: factors } = useUserAuthFactors(getAccessToken());
  const mode = passwordModeFor(factors);

  const handlePasswordChanged = useCallback(() => setPasswordChanged(true), []);

  return (
    <SectionCard
      icon={<PASSWORD_TAB.icon className="h-5 w-5" />}
      primaryText={
        mode === "set"
          ? t("password.setPasswordTitle", "Set a password")
          : t(PASSWORD_TAB.labelKey, PASSWORD_TAB.fallback)
      }
      secondaryText={
        mode === "set"
          ? t(
              "password.setPasswordDescription",
              "You sign in with an emailed code. Add a password to sign in with one as well.",
            )
          : t(
              "password.description",
              "Choose a password you don't reuse anywhere else.",
            )
      }
      allowCollapse={false}
      color="silver"
      variant="outline"
      size="lg"
    >
      <ChangePasswordMainContent
        isPasswordChanged={isPasswordChanged}
        onPasswordChanged={handlePasswordChanged}
        mode={mode}
      />
    </SectionCard>
  );
}
