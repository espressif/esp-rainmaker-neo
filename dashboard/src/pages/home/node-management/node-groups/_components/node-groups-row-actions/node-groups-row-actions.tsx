/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  Button,
  ConfirmationDialog,
} from "@espressif/dashboard-ui-components/components";
import { useDeleteNodeGroupMutation } from "@/api/node-groups";
import type { NodeGroupsRowActionsProps } from "./node-groups-row-actions.props";

export function NodeGroupsRowActions({ groupName }: NodeGroupsRowActionsProps) {
  const { t } = useTranslation(["node-groups", "common"]);
  const deleteMutation = useDeleteNodeGroupMutation();

  const handleConfirm = async () => {
    await deleteMutation.mutateAsync(groupName);
  };

  return (
    <div
      className="opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity"
      onClick={(e) => e.stopPropagation()}
    >
      <ConfirmationDialog
        title={t("deleteNodeGroup.confirmTitle", "Delete node group")}
        description={t("deleteNodeGroup.confirmDescription", "Are you sure you want to delete this node group?", {
          groupName,
          defaultValue:
            "Are you sure you want to delete this node group?",
        })}
        confirmButtonText={t("common:actions.delete", "Delete")}
        cancelButtonText={t("common:actions.cancel", "Cancel")}
        onConfirm={handleConfirm}
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
          aria-label={t("deleteNodeGroup.aria", "Delete node group {{groupName}}", {
            groupName,
            defaultValue: "Delete node group {{groupName}}",
          })}
        >
          {t("common:actions.delete", "Delete")}
        </Button>
      </ConfirmationDialog>
    </div>
  );
}
