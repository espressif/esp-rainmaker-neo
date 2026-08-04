/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Trash2 } from "lucide-react";
import {
  Button,
  ConfirmationDialog,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { useDeleteNodeGroupMutation } from "@/api/node-groups";
import type { DeleteGroupButtonProps } from "./delete-group-button.props";

const GROUPS_LIST_ROUTE = "/home/node-management/node-groups";

export default function DeleteGroupButton({
  groupName,
}: DeleteGroupButtonProps) {
  const { t } = useTranslation(["node-groups", "common"]);
  const navigate = useNavigate();
  const deleteMutation = useDeleteNodeGroupMutation();

  /**
   * Lets `mutateAsync` throw so ConfirmationDialog stays open and surfaces `errorMessage` on
   * failure. The toast and the redirect only run once the group is actually gone — staying on the
   * details page of a deleted group would just render an error state.
   */
  const handleConfirm = useCallback(async () => {
    await deleteMutation.mutateAsync(groupName);
    toast.success(t("details.delete.success", "Node group deleted successfully."));
    void navigate({ to: GROUPS_LIST_ROUTE });
  }, [deleteMutation, groupName, navigate, t]);

  const handleCancel = useCallback(() => {
    deleteMutation.reset();
  }, [deleteMutation]);

  const errorMessage = useMemo(() => {
    if (!deleteMutation.isError) {
      return undefined;
    }
    // AWS SDK errors already carry human-readable text (e.g. the InvalidRequestException raised
    // when a static group still has child groups), so it is shown as-is. `normalizeApiError` is
    // for the REST backend's JSON envelope and would discard this.
    return (
      deleteMutation.error.message?.trim() ||
      t("details.delete.error", "Failed to delete node group.")
    );
  }, [deleteMutation.isError, deleteMutation.error, t]);

  return (
    <ConfirmationDialog
      title={t("details.delete.title", "Delete node group")}
      description={t(
        "details.delete.description",
        "This permanently deletes this node group. This action cannot be undone.",
      )}
      confirmButtonText={t("common:actions.delete", "Delete")}
      cancelButtonText={t("common:actions.cancel", "Cancel")}
      confirmButtonColor="error"
      onConfirm={handleConfirm}
      onCancel={handleCancel}
      isLoading={deleteMutation.isPending}
      error={errorMessage}
    >
      <Button
        type="button"
        color="error"
        variant="outline"
        size="sm"
        fullWidth={false}
        startIcon={<Trash2 className="h-4 w-4" aria-hidden />}
      >
        {t("details.delete.button", "Delete group")}
      </Button>
    </ConfirmationDialog>
  );
}
