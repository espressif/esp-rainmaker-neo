/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Pencil, Sparkles } from "lucide-react";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@espressif/dashboard-ui-components/components";
import type { AlexaSetupTab, AlexaSetupTabsProps } from "./alexa-setup-tabs.props";

const ICON_CLASS = "h-4 w-4 shrink-0";

/**
 * Setup-mode tabs within the Alexa tab. Local state rather than routes, unlike the
 * assistant tabs above them: the two modes are the same configuration seen two ways, not
 * separate pages, and nothing links to one directly.
 */
export default function AlexaSetupTabs({
  activeTab,
  onTabChange,
}: AlexaSetupTabsProps) {
  const { t } = useTranslation("voice-assistants");

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => onTabChange(value as AlexaSetupTab)}
    >
      <TabsList variant="line">
        <TabsTrigger value="automatic">
          <Sparkles className={ICON_CLASS} aria-hidden />
          {t("alexa.setupMode.automaticTab", "Automatic")}
        </TabsTrigger>
        <TabsTrigger value="manual">
          <Pencil className={ICON_CLASS} aria-hidden />
          {t("alexa.setupMode.manualTab", "Manual")}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}
