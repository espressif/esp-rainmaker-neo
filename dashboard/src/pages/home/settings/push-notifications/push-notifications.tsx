/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo, useState } from "react";
import { PageContainer } from "@espressif/dashboard-ui-components/components";
import {
  usePushIntegrations,
  PUSH_INTEGRATION_TYPES,
} from "@/api/integrations";
import PushNotificationsMainContent from "./_components/push-notifications-main-content";
import PushIntegrationFormSheet from "./_components/push-integration-form-sheet";

export default function PushNotifications() {
  const { data, isLoading, error } = usePushIntegrations();
  const [isFormOpen, setIsFormOpen] = useState(false);

  const pushIntegrations = useMemo(
    () =>
      (data?.integrations ?? []).filter((integration) =>
        (PUSH_INTEGRATION_TYPES as string[]).includes(
          integration.integration_type,
        ),
      ),
    [data],
  );

  return (
    <PageContainer maxWidth="xl" noGutters>
      <div className="py-5">
        <PushNotificationsMainContent
          integrations={pushIntegrations}
          isLoading={isLoading}
          error={error}
          onAddIntegration={() => setIsFormOpen(true)}
        />
      </div>

      {isFormOpen && (
        <PushIntegrationFormSheet onClose={() => setIsFormOpen(false)} />
      )}
    </PageContainer>
  );
}
