/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@espressif/dashboard-ui-components/components";
import { NodesListFiltersPanel } from "../nodes-list-filters-panel/nodes-list-filters-panel";
import type { NodesPageHeaderProps } from "./nodes-page-header.props";

export default function NodesPageHeader({ ...props }: NodesPageHeaderProps) {
  const { t } = useTranslation("common");
  const navigate = useNavigate();

  return (
    <div>
      <div className="flex items-center justify-between gap-4 p-5 bg-accent/10 w-full">
        <NodesListFiltersPanel {...props} />
        <Button
          variant="default"
          fullWidth={false}
          startIcon={<Plus className="h-4 w-4" aria-hidden />}
          onClick={() =>
            void navigate({ to: "/home/node-management/register/new" })
          }
          size="sm"
        >
          {t("createMenu.registerNodes", "Register nodes")}
        </Button>
      </div>
    </div>
  );
}
