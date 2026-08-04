/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { User } from "lucide-react";
import {
  Button,
  SimpleClickableCard,
  Tooltip,
  useSidebar,
} from "@espressif/dashboard-ui-components/components";
import { ACCOUNT_SETTINGS_BASE_PATH } from "@/config/account-settings.config";
import { useUserStore } from "@/stores/user.store";

interface SidebarAccountCardProps {
  /**
   * Icon-only rendering, for widths too narrow for the card (collapsed sidebar).
   * Defaults to the sidebar's own collapsed state, so callers rarely pass this.
   */
  minimal?: boolean;
}

/**
 * Account entry pinned to the sidebar footer. The email comes from the persisted
 * user store (`<prefix>-user-storage` in localStorage).
 */
export default function SidebarAccountCard({
  minimal,
}: SidebarAccountCardProps) {
  const { t } = useTranslation("common");
  const navigate = useNavigate();
  const { state, isMobile } = useSidebar();
  const loggedInUserName = useUserStore((s) => s.loggedInUserName);

  const label = t("account", "Account");
  const isMinimal = minimal ?? (state === "collapsed" && !isMobile);
  const goToAccount = () => void navigate({ to: ACCOUNT_SETTINGS_BASE_PATH });

  if (isMinimal) {
    return (
      <Tooltip content={loggedInUserName ?? label} side="right" sideOffset={8}>
        <Button
          size="icon"
          variant="ghost"
          color="secondary"
          aria-label={label}
          onClick={goToAccount}
        >
          <User />
        </Button>
      </Tooltip>
    );
  }

  /**
   * Rendered as a node rather than a string so the email can be `text-xs`;
   * `truncateDescription` only applies to string descriptions and would keep
   * the card's default `body2` size, so the ellipsis is done here instead.
   */
  const email = loggedInUserName ? (
    <Tooltip content={loggedInUserName} side="top">
      <span className="block truncate text-xs">{loggedInUserName}</span>
    </Tooltip>
  ) : undefined;

  return (
    <SimpleClickableCard
      size="sm"
      color="secondary"
      icon={<User />}
      title={label}
      description={email}
      aria-label={label}
      onClick={goToAccount}
      truncateTitle
    />
  );
}
