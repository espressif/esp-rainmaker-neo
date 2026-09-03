/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Alert } from "@espressif/dashboard-ui-components/components";
import { passwordOutcomeCopy } from "../../_utils/signin-errors";
import type { SigninAlertsProps } from "./signin-alerts.props";

/**
 * The alert block every step page renders above its form: a failure wins over
 * the `?reset` success banner, so the two never stack.
 */
export default function SigninAlerts({
  errorMessage,
  resetOutcome,
}: SigninAlertsProps) {
  const { t } = useTranslation("login");
  const outcome = passwordOutcomeCopy(t, resetOutcome);

  if (errorMessage) {
    return (
      <Alert
        title={t("errorTitle", "Unable to login")}
        type="error"
        description={errorMessage}
        hideIcon
        className="border-none shadow-none mb-4"
      />
    );
  }

  if (outcome) {
    return (
      <Alert
        title={outcome.title}
        type="success"
        description={outcome.message}
        hideIcon
        className="border-none shadow-none mb-4"
      />
    );
  }

  return null;
}
