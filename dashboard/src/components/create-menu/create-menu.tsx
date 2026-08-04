/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button, List, Menu } from "@espressif/dashboard-ui-components/components";
import type { ListGroup } from "@espressif/dashboard-ui-components/components";
import {
  CREATABLE_RESOURCES,
  CREATE_RESOURCE_GROUPS,
} from "@/config/create-resources.config";

/**
 * Global quick-create control rendered in the app header. Opens a dropdown of
 * every creatable resource sourced from {@link CREATABLE_RESOURCES}, and navigates
 * to the selected resource's create page.
 */
export default function CreateMenu() {
  const { t } = useTranslation("common");
  const navigate = useNavigate();

  const menuTitle = t("createMenu.title", "Create new");

  const menuGroups = useMemo<ListGroup[]>(
    () =>
      CREATE_RESOURCE_GROUPS.map((groupId) => ({
        id: groupId,
        items: CREATABLE_RESOURCES.filter(
          (resource) => resource.group === groupId,
        ).map((resource) => {
          const Icon = resource.icon;
          return {
            id: resource.id,
            label: t(resource.labelKey, resource.fallback),
            startIcon: <Icon className="h-4 w-4" />,
            onClick: () => navigate({ to: resource.path }),
          };
        }),
      })),
    [navigate, t],
  );

  return (
    <div className="flex items-center justify-end">
      <Menu
        align="end"
        minWidth="220px"
        trigger={
          <Button
            variant="outline"
            color="secondary"
            size="icon"
            className="h-6 w-8"
            startIcon={<Plus />}
            aria-label={menuTitle}
          />
        }
      >
        <List items={menuGroups} role="dropdown-list" />
      </Menu>
    </div>
  );
}
