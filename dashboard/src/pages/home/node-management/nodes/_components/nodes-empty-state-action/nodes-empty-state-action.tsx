/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Sparkles } from "lucide-react";
import { Button } from "@espressif/dashboard-ui-components/components";

export function NodesEmptyStateAction() {
  const { t } = useTranslation("nodes");
  const navigate = useNavigate();

  return (
    <Button
      startIcon={<Sparkles className="h-4 w-4" />}
      onClick={() =>
        void navigate({ to: "/home/node-management/generate" })
      }
    >
      {t("noSearchResults.action", "Generate nodes")}
    </Button>
  );
}
