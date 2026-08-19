/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import { isAlexaEnabled, isGvaEnabled } from "@/lib/config";
import type {
  VoiceAssistantTab,
  VoiceAssistantsPageTabsProps,
} from "./voice-assistants-page-tabs.props";

export default function VoiceAssistantsPageTabs({
  activeTab,
  onTabChange,
}: VoiceAssistantsPageTabsProps) {
  const { t } = useTranslation("common");

  return (
    <Tabs
      value={activeTab}
      onValueChange={(value) => onTabChange(value as VoiceAssistantTab)}
    >
      <TabsList variant="line">
        {/* Each assistant ships as its own stack, so a tab appears only where
            that stack is deployed. */}
        {isAlexaEnabled() && (
          <TabsTrigger value="alexa">
            <CustomIcon type="amazon-alexa" size={16} />
            {t("sidebar.alexa", "Alexa")}
          </TabsTrigger>
        )}
        {isGvaEnabled() && (
          <TabsTrigger value="gva">
            <CustomIcon type="google-assistant" size={16} />
            {t("sidebar.gva", "GVA")}
          </TabsTrigger>
        )}
      </TabsList>
    </Tabs>
  );
}
