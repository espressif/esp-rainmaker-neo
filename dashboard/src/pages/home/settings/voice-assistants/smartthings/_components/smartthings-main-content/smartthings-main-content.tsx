/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { CustomIcon } from "@/components/custom-icon";
import { Plus } from "lucide-react";
import {
  Button,
  FullSizeError,
  Skeleton,
} from "@espressif/dashboard-ui-components/components";
import type { SmartThingsConfigGetResponse } from "@/api/integrations";
import { isNotFoundError, normalizeApiError } from "@/lib/normalize-api-error";
import { SmartThingsConfigurationCard } from "../smartthings-configuration-card";
import type { SmartThingsMainContentProps } from "./smartthings-main-content.props";

function hasConfiguration(
  data: SmartThingsConfigGetResponse | undefined,
): data is SmartThingsConfigGetResponse {
  return Boolean(data?.client_id);
}

export default function SmartThingsMainContent({
  data,
  isLoading,
  error,
  onConfigure,
}: SmartThingsMainContentProps) {
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
        title={t("smartthings.fetchError", "Failed to load SmartThings configuration")}
        illustration={<CustomIcon type="smartthings" size={48} />}
      >
        {normalizeApiError(
          error,
          t("smartthings.fetchError", "Failed to load SmartThings configuration"),
        )}
      </FullSizeError>
    );
  }

  if (!hasConfiguration(data)) {
    return (
      <FullSizeError
        title={t("smartthings.notConfiguredTitle", "SmartThings not configured")}
        illustration={<CustomIcon type="smartthings" size={48} />}
      >
        <div className="flex flex-col items-center gap-4">
          <span>
            {t(
              "smartthings.notConfiguredDescription",
              "No SmartThings configuration found yet.",
            )}
          </span>
          <Button
            type="button"
            variant="default"
            fullWidth={false}
            onClick={onConfigure}
            startIcon={<Plus className="h-4 w-4" aria-hidden />}
          >
            {t("smartthings.configureButton", "Configure")}
          </Button>
        </div>
      </FullSizeError>
    );
  }

  return <SmartThingsConfigurationCard config={data} onEdit={onConfigure} />;
}
