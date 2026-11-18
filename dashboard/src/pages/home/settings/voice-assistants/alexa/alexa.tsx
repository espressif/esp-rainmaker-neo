/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useState } from "react";
import { useGetAlexaConfig } from "@/api/integrations";
import { AlexaMainContent } from "./_components/alexa-main-content";
import { AlexaAutomaticContent } from "./_components/alexa-automatic-content";
import { AlexaConfigFormSheet } from "./_components/alexa-config-form-sheet";
import { AlexaCreateSkillFormSheet } from "./_components/alexa-create-skill-form-sheet";
import { AlexaSetupTabs, type AlexaSetupTab } from "./_components/alexa-setup-tabs";
import {
  failedSetup,
  isSetupFinished,
  runningSetupSkillId,
} from "./_utils/alexa-setup-status";

/** How often to re-read the config while a created skill is still being configured. */
const PENDING_POLL_MS = 10_000;

export default function Alexa() {
  const [activeTab, setActiveTab] = useState<AlexaSetupTab>("automatic");
  const [isFormOpen, setIsFormOpen] = useState(false);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [pendingSkillId, setPendingSkillId] = useState<string>();

  // Polls while a run is pending here or, read from config_status, was started elsewhere or before a reload.
  const { data, dataUpdatedAt, isLoading, error } = useGetAlexaConfig({
    refetchInterval: (latest, updatedAt) =>
      pendingSkillId || runningSetupSkillId(latest?.config_status, updatedAt)
        ? PENDING_POLL_MS
        : false,
  });

  const setupStatus = data?.config_status;
  const activeSkillId = pendingSkillId ?? runningSetupSkillId(setupStatus, dataUpdatedAt);

  // dataUpdatedAt stands in for "now": it advances with every poll and keeps Date.now() out of render.
  useEffect(() => {
    if (pendingSkillId && isSetupFinished(setupStatus, pendingSkillId, dataUpdatedAt)) {
      setPendingSkillId(undefined);
    }
  }, [setupStatus, pendingSkillId, dataUpdatedAt]);

  return (
    <div className="flex flex-col gap-4 py-6">
      <AlexaSetupTabs activeTab={activeTab} onTabChange={setActiveTab} />

      {activeTab === "manual" ? (
        <AlexaMainContent
          data={data}
          isLoading={isLoading}
          error={error}
          onConfigure={() => setIsFormOpen(true)}
        />
      ) : (
        <AlexaAutomaticContent
          data={data}
          isLoading={isLoading}
          error={error}
          onCreateSkill={() => setIsCreateOpen(true)}
          pendingSkillId={activeSkillId}
          pendingStage={setupStatus?.ref_id === activeSkillId ? setupStatus?.stage : undefined}
          failedStatus={failedSetup(setupStatus, activeSkillId, dataUpdatedAt)}
        />
      )}

      {isFormOpen && (
        <AlexaConfigFormSheet
          initialData={data}
          onClose={() => setIsFormOpen(false)}
        />
      )}

      {isCreateOpen && (
        <AlexaCreateSkillFormSheet
          onClose={() => setIsCreateOpen(false)}
          onCreated={(skillId) => {
            setPendingSkillId(skillId);
            setIsCreateOpen(false);
          }}
        />
      )}
    </div>
  );
}
