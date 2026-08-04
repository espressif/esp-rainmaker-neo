/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import {
  Button,
  FullSizeError,
  Skeleton,
} from "@espressif/dashboard-ui-components/components";
import type { AlexaConfigGetResponse } from "@/api/integrations";
import { CustomIcon } from "@/components/custom-icon";
import { isNotFoundError, normalizeApiError } from "@/lib/normalize-api-error";
import { AlexaConfigurationCard } from "../alexa-configuration-card";
import type { AlexaMainContentProps } from "./alexa-main-content.props";

function hasConfiguration(
  data: AlexaConfigGetResponse | undefined,
): data is AlexaConfigGetResponse {
  if (!data) {
    return false;
  }
  return Boolean(
    data.client_id || data.skill_id || (data.redirect_uris?.length ?? 0) > 0,
  );
}

/**
 * Renders exactly one of the Alexa tab states via early returns (per the
 * dashboard rendering rule): loading, fetch error, not-configured, or the
 * saved-configuration card. A 404 (or an empty payload) means "not configured
 * yet" rather than a fault.
 */
export default function AlexaMainContent({
  data,
  isLoading,
  error,
  onConfigure,
}: AlexaMainContentProps) {
  const { t } = useTranslation("voice-assistants");

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }

  if (error && !isNotFoundError(error)) {
    return (
      <FullSizeError
        title={t("alexa.fetchError", "Failed to load Alexa configuration")}
        illustration={<CustomIcon type="amazon-alexa" size={48} />}
      >
        {normalizeApiError(error, t("alexa.fetchError", "Failed to load Alexa configuration"))}
      </FullSizeError>
    );
  }

  if (!hasConfiguration(data)) {
    return (
      <FullSizeError
        title={t("alexa.notConfiguredTitle", "Alexa not configured")}
        illustration={<CustomIcon type="amazon-alexa" size={48} />}
      >
        <div className="flex flex-col items-center gap-4">
          <span>{t("alexa.notConfiguredDescription", "No Alexa configuration found yet.")}</span>
          <Button
            type="button"
            variant="default"
            fullWidth={false}
            onClick={onConfigure}
            startIcon={<Plus className="h-4 w-4" aria-hidden />}
          >
            {t("alexa.configureButton", "Configure")}
          </Button>
        </div>
      </FullSizeError>
    );
  }

  return <AlexaConfigurationCard config={data} onEdit={onConfigure} />;
}
