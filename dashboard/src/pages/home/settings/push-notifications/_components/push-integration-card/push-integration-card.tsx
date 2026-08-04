/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { FlaskConical, Trash2 } from "lucide-react";
import {
  IconTextActionCard,
  Badge,
  Button,
  ConfirmationDialog,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { useDeletePushIntegration } from "@/api/integrations";
import { CustomIcon } from "@/components/custom-icon";
import {
  getPushIntegrationPresentation,
  isSandboxIntegration,
  getIntegrationDescriptor,
} from "@/config/push-integration.config";
import type { PushIntegrationCardProps } from "./push-integration-card.props";

export default function PushIntegrationCard({
  integration,
}: PushIntegrationCardProps) {
  const { t } = useTranslation(["push-notifications", "common"]);
  const deleteMutation = useDeletePushIntegration();

  const { iconType } = getPushIntegrationPresentation(
    integration.integration_type,
  );
  const descriptor = getIntegrationDescriptor(integration);
  const isSandbox = isSandboxIntegration(integration.integration_type);

  // Errors are surfaced as a toast rather than rethrown so the dialog closes.
  const handleDelete = async () => {
    try {
      await deleteMutation.mutateAsync(integration.integration_id);
      toast.success(t("deleteSuccess", "Push integration deleted successfully."));
    } catch {
      toast.error(t("deleteError", "Failed to delete push integration."));
    }
  };

  return (
    <IconTextActionCard
      icon={<CustomIcon type={iconType} size={20} aria-hidden />}
      title={integration.integration_id}
      truncateTitle
      description={
        descriptor ? (
          <span className="italic text-sm text-muted-foreground">
            {t(descriptor.i18nLabelKey)}: {descriptor.value}
          </span>
        ) : undefined
      }
      color="mist"
      variant={isSandbox ? "soft" : "outline"}
      size="lg"
      actions={
        <div className="flex items-center gap-2">
          {isSandbox && (
            <Badge
              variant="outline"
              color="mist"
              className="font-normal gap-1.5 bg-background"
            >
              <FlaskConical className="h-3.5 w-3.5 shrink-0" aria-hidden />
              {t("sandboxBadge", "SANDBOX")}
            </Badge>
          )}
          <ConfirmationDialog
            title={t("deleteConfirmTitle", "Delete integration?")}
            description={t("deleteConfirmDescription", "This will delete {{id}} and its SNS platform application. Devices registered against it will stop receiving notifications.", {
              id: integration.integration_id,
            })}
            confirmButtonText={t("common:actions.delete", "Delete")}
            confirmButtonColor="error"
            onConfirm={handleDelete}
            onCancel={() => {}}
            isLoading={deleteMutation.isPending}
          >
            <Button
              size="icon"
              variant="ghost"
              color="error"
              fullWidth={false}
              aria-label={t("common:actions.delete", "Delete")}
            >
              <Trash2 className="h-4 w-4" aria-hidden />
            </Button>
          </ConfirmationDialog>
        </div>
      }
    />
  );
}
