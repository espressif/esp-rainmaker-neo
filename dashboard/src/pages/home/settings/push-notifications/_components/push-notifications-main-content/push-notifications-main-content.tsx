/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { BellRing, Plus } from "lucide-react";
import {
  SectionCard,
  Button,
} from "@espressif/dashboard-ui-components/components";
import PushNotificationsMainContentBody from "./push-notifications-main-content-body";
import type { PushNotificationsMainContentProps } from "./push-notifications-main-content.props";

export default function PushNotificationsMainContent({
  integrations,
  isLoading,
  error,
  onAddIntegration,
}: PushNotificationsMainContentProps) {
  const { t } = useTranslation("push-notifications");

  return (
    <SectionCard
      icon={<BellRing className="h-5 w-5" aria-hidden />}
      primaryText={t("heading", "Push Notifications")}
      secondaryText={t("secondaryText", "Manage push notification integrations (APNS / FCM) used to deliver notifications to your apps.")}
      allowCollapse={false}
      color="silver"
      variant="outline"
      size="lg"
      actions={
        <Button
          type="button"
          variant="default"
          fullWidth={false}
          startIcon={<Plus className="h-4 w-4" aria-hidden />}
          onClick={onAddIntegration}
        >
          {t("addIntegrationButton", "Add Integration")}
        </Button>
      }
    >
      <PushNotificationsMainContentBody
        integrations={integrations}
        isLoading={isLoading}
        error={error}
      />
    </SectionCard>
  );
}
