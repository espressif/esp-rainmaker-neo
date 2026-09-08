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
  SectionCard,
  Skeleton,
} from "@espressif/dashboard-ui-components/components";
import type { GvaConfigGetResponse } from "@/api/integrations";
import { CustomIcon } from "@/components/custom-icon";
import { isNotFoundError, normalizeApiError } from "@/lib/normalize-api-error";
import { GvaConfigurationCard } from "../gva-configuration-card";
import type { GvaMainContentProps } from "./gva-main-content.props";

function hasConfiguration(
  data: GvaConfigGetResponse | undefined,
): data is GvaConfigGetResponse {
  if (!data) {
    return false;
  }
  return Boolean(data.project_id || data.client_email);
}

/**
 * Renders exactly one of the GVA tab states via early returns (per the
 * dashboard rendering rule): loading, fetch error, not-configured, or the
 * saved-configuration card. A 404 (or an empty payload) means "not configured
 * yet" rather than a fault, and offers a Configure action that opens the same
 * sheet used to edit an existing configuration.
 */
export default function GvaMainContent({
  data,
  isLoading,
  error,
  onConfigure,
}: GvaMainContentProps) {
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
      <SectionCard
        variant="outline"
        color="error"
        primaryText={normalizeApiError(
          error,
          t("gva.fetchError", "Failed to load GVA configuration"),
        )}
        icon={<CustomIcon type="google-assistant" size={20} aria-hidden />}
        allowCollapse={false}
      />
    );
  }

  if (!hasConfiguration(data)) {
    return (
      <SectionCard allowCollapse={false} variant="soft" color="silver">
        <FullSizeError
          title={t("gva.notConfiguredTitle", "GVA not configured")}
          illustration={<CustomIcon type="google-assistant" size={48} />}
        >
          <div className="flex flex-col items-center gap-4">
            <span>
              {t(
                "gva.notConfiguredDescription",
                "No Google Voice Assistant configuration found yet.",
              )}
            </span>
            <Button
              type="button"
              variant="default"
              fullWidth={false}
              onClick={onConfigure}
              startIcon={<Plus className="h-4 w-4" aria-hidden />}
            >
              {t("gva.configureButton", "Configure")}
            </Button>
          </div>
        </FullSizeError>
      </SectionCard>
    );
  }

  return <GvaConfigurationCard config={data} onEdit={onConfigure} />;
}
