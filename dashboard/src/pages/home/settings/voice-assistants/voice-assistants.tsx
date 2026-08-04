/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import {
  ContentContainer,
  PageContainer,
} from "@espressif/dashboard-ui-components/components";
import { useTranslation } from "react-i18next";
import { VoiceAssistantsPageHeading } from "./_components/voice-assistants-page-heading";
import { ConfigurationValues } from "./_components/configuration-values";
import type { VoiceAssistantTab } from "./_components/voice-assistants-page-tabs";

const TAB_PATHS: Record<VoiceAssistantTab, string> = {
  alexa: "/home/settings/voice-assistants/alexa",
  gva: "/home/settings/voice-assistants/gva",
};

function getActiveTab(pathname: string): VoiceAssistantTab {
  return pathname.includes("/voice-assistants/gva") ? "gva" : "alexa";
}

/**
 * Route-backed tab shell for the voice assistant pages. The tab bar lives in
 * the elevated page heading, reflects the active child route, and navigates on
 * change; the tab bodies (`alexa`, `gva`) render through <Outlet/> as their own
 * routes.
 */
export default function VoiceAssistants() {
  const { t } = useTranslation("voice-assistants");
  const location = useLocation();
  const navigate = useNavigate();

  const activeTab = useMemo(
    () => getActiveTab(location.pathname),
    [location.pathname],
  );

  const handleTabChange = useCallback(
    (value: VoiceAssistantTab) => {
      void navigate({ to: TAB_PATHS[value] });
    },
    [navigate],
  );

  return (
    <PageContainer
      noGutters
      className="p-0"
      elevateHeading
      heading={
        <VoiceAssistantsPageHeading
          activeTab={activeTab}
          onTabChange={handleTabChange}
        />
      }
    >
      <ContentContainer maxWidth="xl" noGutters>
        <Outlet />
        {/* Reference material, so it follows the assistant's own configuration and starts
            collapsed. It lives in the shell because these values hold whether or not either
            assistant is configured yet. */}
        <div className="pb-6">
          <ConfigurationValues
            title={t("configurationValues", "Configuration values")}
            activeTab={activeTab}
          />
        </div>
      </ContentContainer>
    </PageContainer>
  );
}
