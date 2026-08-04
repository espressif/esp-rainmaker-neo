/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Alert } from "@espressif/dashboard-ui-components/components";

/**
 * Sets expectations before the user changes anything: preferences on this page live in the
 * browser (see the persisted store in [app.store.ts](@/stores/app.store)), not on their
 * account, so they do not follow the user to another browser or device.
 */
export default function PreferencesScopeNotice() {
  const { t } = useTranslation("account-settings");

  return (
    <Alert
      type="info"
      variant="soft"
      color="info"
      size="sm"
      title={t(
        "preferences.scopeNotice.title",
        "Saved on this browser only",
      )}
      description={t(
        "preferences.scopeNotice.description",
        "These preferences stay on this browser instead of your account, so they won't carry over if you sign in from another browser or device.",
      )}
    />
  );
}
