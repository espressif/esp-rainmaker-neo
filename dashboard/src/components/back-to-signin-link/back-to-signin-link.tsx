/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeftIcon } from "lucide-react";
import { Link } from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import type { BackToSignInLinkProps } from "./back-to-signin-link.props";

/**
 * The "Back to sign in" control shared by every unauthenticated page — the
 * sign-in step screens and the password-reset flow — so the label and styling
 * stay in one place. The current search params ride along with the navigation,
 * which is what keeps `?redirect`/`?reset` alive across the sign-in steps.
 */
export default function BackToSignInLink({
  to = "/login",
}: BackToSignInLinkProps) {
  const { t } = useTranslation("common");
  const location = useLocation();

  return (
    <Link
      to={to}
      search={location.search as Record<string, unknown>}
      linkComponent={TanstackRouterLink}
      color="primary"
      underline={false}
      startIcon={<ArrowLeftIcon className="w-4 h-4" />}
    >
      {t("backToSignin", "Back to sign in")}
    </Link>
  );
}
