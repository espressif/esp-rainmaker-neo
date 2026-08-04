/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ArrowLeftIcon } from "lucide-react";
import { Link } from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";

/**
 * Escape hatch back to `/login`, shared by every page in the password-reset
 * flow so the label and styling stay in one place.
 */
export default function BackToSignInLink() {
  const { t } = useTranslation("common");

  return (
    <Link
      to="/login"
      linkComponent={TanstackRouterLink}
      color="primary"
      underline={false}
      startIcon={<ArrowLeftIcon className="w-4 h-4" />}
    >
      {t("backToSignin", "Back to sign in")}
    </Link>
  );
}
