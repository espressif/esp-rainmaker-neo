/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Sparkles } from "lucide-react";
import {
  Button,
  MonospaceContent,
  SectionCard,
  Skeleton,
  Spinner,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { CustomIcon } from "@/components/custom-icon";
import { isNotFoundError, normalizeApiError } from "@/lib/normalize-api-error";
import type { AlexaSetupStatus } from "@/api/integrations";
import type { AlexaAutomaticContentProps } from "./alexa-automatic-content.props";

function SetupFailedNote({ status }: { status: AlexaSetupStatus }) {
  const { t } = useTranslation("voice-assistants");
  const stage = status.stage ?? "-";
  return (
    <Typography variant="body2" as="p" className="text-destructive">
      {status.status === "FAILED"
        ? t(
            "alexa.autoCreate.failedDescription",
            'Configuration failed at "{{stage}}": {{error}}',
            {
              stage,
              error: status.error ?? "-",
            },
          )
        : t(
            "alexa.autoCreate.stalledDescription",
            'Configuration stopped responding at "{{stage}}". Create the skill again.',
            {
              stage,
            },
          )}
    </Typography>
  );
}

/**
 * Automatic mode's half of the Alexa tab, laid out like the manual card it sits beside:
 * one SectionCard whose action lives top-right. A 404 (or an empty payload) means "no
 * skill created yet" rather than a fault, same as the manual side reads it.
 */
export default function AlexaAutomaticContent({
  data,
  isLoading,
  error,
  onCreateSkill,
  pendingSkillId,
  pendingStage,
  failedStatus,
}: AlexaAutomaticContentProps) {
  const { t } = useTranslation(["voice-assistants", "common"]);
  const shownSkillId = pendingSkillId ?? failedStatus?.ref_id ?? data?.skill_id;

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }

  if (error && !isNotFoundError(error)) {
    return (
      <SectionCard
        variant="outline"
        color="error"
        primaryText={normalizeApiError(
          error,
          t("alexa.fetchError", "Failed to load Alexa configuration"),
        )}
        icon={<CustomIcon type="amazon-alexa" size={20} aria-hidden />}
        allowCollapse={false}
      />
    );
  }

  return (
    <SectionCard
      icon={<CustomIcon type="amazon-alexa" size={24} aria-hidden />}
      primaryText={t("alexa.autoCreate.heading", "Automatic Alexa Configuration")}
      secondaryText={t(
        "alexa.autoCreate.secondaryText",
        "Creates or updates your Alexa skill and connects it to Neo in one step",
      )}
      allowCollapse={false}
      color="mist"
      variant="outline"
      size="lg"
      actions={
        <Button
          type="button"
          variant="default"
          color="primary"
          size="lg"
          fullWidth={false}
          onClick={onCreateSkill}
          disabled={Boolean(pendingSkillId)}
          startIcon={<Sparkles className="h-4 w-4" aria-hidden />}
        >
          {t("alexa.autoCreate.startButton", "Set up Alexa")}
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        {pendingSkillId ? (
          <div className="flex items-center gap-3">
            <Spinner size={16} />
            <Typography variant="body2" as="p">
              {t(
                "alexa.autoCreate.pendingDescription",
                "The skill is being configured. This usually takes a minute or two.",
              )}
              {pendingStage
                ? ` ${t("alexa.autoCreate.pendingStage", "Current step: {{stage}}.", { stage: pendingStage })}`
                : null}
            </Typography>
          </div>
        ) : null}

        {failedStatus ? <SetupFailedNote status={failedStatus} /> : null}

        {shownSkillId ? (
          <MonospaceContent
            title={t("alexa.skillId", "Skill ID")}
            value={shownSkillId}
            color="mist"
          />
        ) : (
          <Typography variant="body2" as="p" className="text-muted-foreground">
            {t(
              "alexa.autoCreate.emptyDescription",
              "No skill created from here yet. You need a Login with Amazon security profile.",
            )}
          </Typography>
        )}
      </div>
    </SectionCard>
  );
}
