/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CloudDownload, Server } from "lucide-react";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@espressif/dashboard-ui-components/components";

export type GroupDetailsTab = "nodes" | "ota-jobs";

interface GroupDetailsPageTabsProps {
  activeTab: GroupDetailsTab;
  onTabChange: (value: GroupDetailsTab) => void;
}

export default function GroupDetailsPageTabs({
  activeTab,
  onTabChange,
}: GroupDetailsPageTabsProps) {
  const { t } = useTranslation("node-groups");

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => onTabChange(value as GroupDetailsTab)}
    >
      <TabsList variant="line">
        <TabsTrigger value="nodes">
          <Server className="h-4 w-4" aria-hidden />
          {t("details.tabs.nodes", "Nodes")}
        </TabsTrigger>
        <TabsTrigger value="ota-jobs">
          <CloudDownload className="h-4 w-4" aria-hidden />
          {t("details.tabs.otaJobs", "OTA Jobs")}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}
