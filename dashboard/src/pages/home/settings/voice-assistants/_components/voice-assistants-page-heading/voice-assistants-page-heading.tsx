/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { InternalPageHeader } from "@/components/internal-page-header";
import { VoiceAssistantsPageTabs } from "../voice-assistants-page-tabs";
import type { VoiceAssistantsPageHeadingProps } from "./voice-assistants-page-heading.props";

export default function VoiceAssistantsPageHeading({
  activeTab,
  onTabChange,
}: VoiceAssistantsPageHeadingProps) {
  return (
    <InternalPageHeader
      footer={
        <VoiceAssistantsPageTabs
          activeTab={activeTab}
          onTabChange={onTabChange}
        />
      }
    />
  );
}
