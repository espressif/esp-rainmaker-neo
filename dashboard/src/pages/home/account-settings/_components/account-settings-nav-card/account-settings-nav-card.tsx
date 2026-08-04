/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight } from "lucide-react";
import {
  List,
  ScrollArea,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import type { ListGroup } from "@espressif/dashboard-ui-components/components";
import { ACCOUNT_SETTINGS_TABS } from "@/config/account-settings.config";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import type { AccountSettingsNavCardProps } from "./account-settings-nav-card.props";

/**
 * Desktop nav rail for the account settings tabs. Each tab is its own `ListGroup` so a
 * separator is drawn between every row — `List` only separates groups, never items
 * within a group.
 *
 * Rows carry `href`, so they render as real links: right-click, middle-click and
 * modifier-click behave like the app sidebar, and the active row gets
 * `aria-current="page"`. Navigation is therefore the link's job, not a click handler's.
 */
export default function AccountSettingsNavCard({
  activeTabId,
}: AccountSettingsNavCardProps) {
  const { t } = useTranslation("account-settings");

  const navGroups = useMemo<ListGroup[]>(
    () =>
      ACCOUNT_SETTINGS_TABS.map((tab) => {
        const Icon = tab.icon;
        const isSelected = tab.id === activeTabId;
        return {
          id: tab.id,
          items: [
            {
              id: tab.id,
              label: t(tab.labelKey, tab.fallback),
              startIcon: <Icon className="h-5 w-5 shrink-0" />,
              endIcon: <ChevronRight className="h-5 w-5 shrink-0" />,
              isSelected,
              href: tab.path,
              // Per-item className is merged last, so these replace the
              // component's own `bg-accent font-medium` selected styling
              // outright rather than layering over it.
              className: isSelected ? "bg-mist font-bold" : undefined,
            },
          ],
        };
      }),
    [activeTabId, t],
  );

  return (
    <SectionCard
      allowCollapse={false}
      color="mist"
      variant="outline"
      className="[&_.section-card-body]:p-0"
    >
      <nav aria-label={t("navAriaLabel", "Account settings sections")}>
        {/* Bounded height keeps a long tab list scrolling inside the card rather than stretching the page. */}
        <ScrollArea className="max-h-[calc(100dvh-18rem)]">
          {/* Rows run edge to edge and are separated by the group dividers alone. */}
          <List
            items={navGroups}
            linkComponent={TanstackRouterLink}
            itemClassName="m-0 rounded-none p-4"
            separatorClassName="m-0"
          />
        </ScrollArea>
      </nav>
    </SectionCard>
  );
}
