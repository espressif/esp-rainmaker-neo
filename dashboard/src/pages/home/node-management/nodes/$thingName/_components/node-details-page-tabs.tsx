/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CloudDownload, LayoutDashboard, List, Tag } from "lucide-react";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@espressif/dashboard-ui-components/components";

export type NodeDetailsTab = "overview" | "tags" | "attributes" | "ota-jobs";

interface NodeDetailsPageTabsProps {
  activeTab: NodeDetailsTab;
  onTabChange: (value: NodeDetailsTab) => void;
}

export default function NodeDetailsPageTabs({
  activeTab,
  onTabChange,
}: NodeDetailsPageTabsProps) {
  const { t } = useTranslation("nodes");

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => onTabChange(value as NodeDetailsTab)}
    >
      <TabsList variant="line">
        <TabsTrigger value="overview">
          <LayoutDashboard className="h-4 w-4" aria-hidden />
          {t("details.tabs.overview", "Overview")}
        </TabsTrigger>
        <TabsTrigger value="tags">
          <Tag className="h-4 w-4" aria-hidden />
          {t("details.tabs.tags", "Tags")}
        </TabsTrigger>
        <TabsTrigger value="attributes">
          <List className="h-4 w-4" aria-hidden />
          {t("details.tabs.attributes", "Attributes")}
        </TabsTrigger>
        <TabsTrigger value="ota-jobs">
          <CloudDownload className="h-4 w-4" aria-hidden />
          {t("details.tabs.otaJobs", "OTA Jobs")}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}
