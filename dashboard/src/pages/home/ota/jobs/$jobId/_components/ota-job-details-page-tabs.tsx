/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Boxes, LayoutDashboard } from "lucide-react";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@espressif/dashboard-ui-components/components";

export type OtaJobDetailsTab = "overview" | "nodes";

interface OtaJobDetailsPageTabsProps {
  activeTab: OtaJobDetailsTab;
  onTabChange: (value: OtaJobDetailsTab) => void;
}

export default function OtaJobDetailsPageTabs({
  activeTab,
  onTabChange,
}: OtaJobDetailsPageTabsProps) {
  const { t } = useTranslation("ota-jobs");

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => onTabChange(value as OtaJobDetailsTab)}
    >
      <TabsList variant="line">
        <TabsTrigger value="overview">
          <LayoutDashboard className="h-4 w-4" aria-hidden />
          {t("details.tabs.overview", "Overview")}
        </TabsTrigger>
        <TabsTrigger value="nodes">
          <Boxes className="h-4 w-4" aria-hidden />
          {t("details.tabs.nodes", "Nodes")}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}
