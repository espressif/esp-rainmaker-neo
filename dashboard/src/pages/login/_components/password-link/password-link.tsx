/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Button } from "@espressif/dashboard-ui-components/components";
import type { PasswordLinkProps } from "./password-link.props";

/** Small and grey beneath the main button: the emailed code is the default, the password the rare way in. */
export default function PasswordLink({ onUsePassword }: PasswordLinkProps) {
  const { t } = useTranslation(["login", "common"]);

  return (
    <Button
      type="button"
      variant="link"
      size="sm"
      fullWidth={false}
      className="self-center px-0 h-auto text-muted-foreground"
      onClick={onUsePassword}
    >
      {t("usePasswordInstead", "Use a password instead")}
    </Button>
  );
}
