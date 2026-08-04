/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  NoDataCard,
  Skeleton,
} from "@espressif/dashboard-ui-components/components";
import type { IntegrationDetail } from "@/api/integrations";
import PushIntegrationCard from "../push-integration-card";

interface PushNotificationsMainContentBodyProps {
  integrations: IntegrationDetail[];
  isLoading: boolean;
  error: Error | null;
}

export default function PushNotificationsMainContentBody({
  integrations,
  isLoading,
  error,
}: PushNotificationsMainContentBodyProps) {
  const { t } = useTranslation("push-notifications");

  if (isLoading) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }

  if (error) {
    return (
      <Alert
        type="error"
        variant="outline"
        title={t("fetchError", "Failed to load push integrations")}
        description={error.message}
      />
    );
  }

  if (integrations.length === 0) {
    return <NoDataCard heading={t("noIntegrations", "No push integrations configured yet.")} />;
  }

  return (
    <div className="flex flex-col gap-3">
      {integrations.map((integration) => (
        <PushIntegrationCard
          key={integration.integration_id}
          integration={integration}
        />
      ))}
    </div>
  );
}
