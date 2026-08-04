/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Ban, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Button,
  ButtonGroup,
  ConfirmationDialog,
} from "@espressif/dashboard-ui-components/components";
import {
  isCancelableJobStatus,
  isDeletableJobStatus,
} from "@/config/ota-job-status.config";
import { stripOtaPrefix } from "@/aws/services/ota.service";
import {
  useCancelOtaJobMutation,
  useDeleteOtaJobMutation,
} from "@/api/ota-jobs";
import type { OtaJobsRowActionsProps } from "./ota-jobs-row-actions.props";

export function OtaJobsRowActions({ jobId, status }: OtaJobsRowActionsProps) {
  const { t } = useTranslation(["ota-jobs", "common"]);
  const displayName = stripOtaPrefix(jobId);
  const showCancel = isCancelableJobStatus(status);
  const showDelete = isDeletableJobStatus(status);

  const cancelMutation = useCancelOtaJobMutation();
  const deleteMutation = useDeleteOtaJobMutation();

  const handleCancel = async () => {
    await cancelMutation.mutateAsync(jobId);
  };

  const handleDelete = async () => {
    await deleteMutation.mutateAsync(jobId);
  };

  return (
    <div
      className="opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity"
      onClick={(e) => e.stopPropagation()}
    >
      <ButtonGroup>
        {showCancel && (
          <ConfirmationDialog
            title={t("cancelOtaJob.confirmTitle", "Cancel OTA job")}
            description={t("cancelOtaJob.confirmDescription", "Are you sure you want to cancel OTA job \"{{jobId}}\"? This will stop the job from being sent to more nodes. Nodes already in progress will not be affected.", {
              jobId: displayName,
            })}
            confirmButtonText={t("cancelOtaJob.confirmButton", "Cancel Job")}
            cancelButtonText={t("cancelOtaJob.cancelButton", "Go Back")}
            onConfirm={handleCancel}
            onCancel={() => {}}
            isLoading={cancelMutation.isPending}
            error={
              cancelMutation.error instanceof Error
                ? cancelMutation.error.message
                : undefined
            }
          >
            <Button
              type="button"
              color="gray"
              variant="outline"
              size="sm"
              fullWidth={false}
              startIcon={<Ban className="h-4 w-4" aria-hidden />}
              aria-label={t("cancelOtaJob.aria", "Cancel OTA job {{jobId}}", { jobId })}
            >
              {t("cancelOtaJob.confirmButton", "Cancel Job")}
            </Button>
          </ConfirmationDialog>
        )}
        {showDelete && (
          <ConfirmationDialog
            title={t("deleteOtaJob.confirmTitle", "Delete OTA job")}
            description={t("deleteOtaJob.confirmDescription", "Are you sure you want to delete OTA job \"{{jobId}}\"? This will also delete the associated stream and AWS IoT job.", {
              jobId: displayName,
            })}
            confirmButtonText={t("common:actions.delete", "Delete")}
            cancelButtonText={t("common:actions.cancel", "Cancel")}
            onConfirm={handleDelete}
            onCancel={() => {}}
            isLoading={deleteMutation.isPending}
            error={
              deleteMutation.error instanceof Error
                ? deleteMutation.error.message
                : undefined
            }
          >
            <Button
              type="button"
              color="error"
              variant="outline"
              size="sm"
              fullWidth={false}
              startIcon={<Trash2 className="h-4 w-4" aria-hidden />}
              aria-label={t("deleteOtaJob.aria", "Delete OTA job {{jobId}}", { jobId })}
            >
              {t("common:actions.delete", "Delete")}
            </Button>
          </ConfirmationDialog>
        )}
      </ButtonGroup>
    </div>
  );
}
