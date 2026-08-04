/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { SectionCard } from "@espressif/dashboard-ui-components/components";
import { ACCOUNT_SETTINGS_TABS_BY_ID } from "@/config/account-settings.config";
import { ChangePasswordMainContent } from "./_components/change-password-main-content";

const PASSWORD_TAB = ACCOUNT_SETTINGS_TABS_BY_ID.password;

/**
 * Change password tab. Holds only whether the change has already succeeded — the form
 * owns its own validation, mutation and failure state.
 *
 * The section shell (`account-settings.tsx`) supplies `PageContainer` and the page
 * heading, so this tab renders a bare card.
 */
export default function Password() {
  const { t } = useTranslation("account-settings");
  const [isPasswordChanged, setPasswordChanged] = useState(false);

  const handlePasswordChanged = useCallback(() => setPasswordChanged(true), []);

  return (
    <SectionCard
      icon={<PASSWORD_TAB.icon className="h-5 w-5" />}
      primaryText={t(PASSWORD_TAB.labelKey, PASSWORD_TAB.fallback)}
      secondaryText={t(
        "password.description",
        "Choose a password you don't reuse anywhere else.",
      )}
      allowCollapse={false}
      color="silver"
      variant="outline"
      size="lg"
    >
      <ChangePasswordMainContent
        isPasswordChanged={isPasswordChanged}
        onPasswordChanged={handlePasswordChanged}
      />
    </SectionCard>
  );
}
