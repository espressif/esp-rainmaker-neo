/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import {
  Button,
  ConfirmationDialog,
  toast,
} from "@espressif/dashboard-ui-components/components";
import { useRemoveThingFromGroupMutation } from "@/api/node-groups";
import { normalizeApiError } from "@/lib/normalize-api-error";

interface GroupNodesRowActionsProps {
  groupName: string;
  thingName: string;
}

export default function GroupNodesRowActions({
  groupName,
  thingName,
}: GroupNodesRowActionsProps) {
  const { t } = useTranslation(["node-groups", "common"]);
  const mutation = useRemoveThingFromGroupMutation();

  // Rejects on failure, so the toast is skipped and the dialog stays open with
  // the inline error below.
  const handleConfirm = useCallback(async () => {
    await mutation.mutateAsync({ groupName, thingName });
    toast.success({
      title: t(
        "details.nodes.remove.success",
        'Node "{{thingName}}" removed from this group.',
        { thingName },
      ),
      children: t(
        "common:toast.listRefreshNote",
        "The list updates automatically in a few seconds.",
      ),
    });
  }, [mutation, groupName, thingName, t]);

  const handleCancel = useCallback(() => {
    mutation.reset();
  }, [mutation]);

  const stopPropagation = useCallback(
    (event: React.SyntheticEvent) => event.stopPropagation(),
    [],
  );

  return (
    <div onClick={stopPropagation} onKeyDown={stopPropagation}>
      <ConfirmationDialog
        title={t(
          "details.nodes.remove.confirmTitle",
          "Remove node from group",
        )}
        description={t(
          "details.nodes.remove.confirmDescription",
          "Are you sure you want to remove this node from this group?",
        )}
        confirmButtonText={t(
          "common:actions.remove",
          "Remove",
        )}
        cancelButtonText={t(
          "common:actions.cancel",
          "Cancel",
        )}
        confirmButtonColor="error"
        onConfirm={handleConfirm}
        onCancel={handleCancel}
        isLoading={mutation.isPending}
        error={
          mutation.error
            ? normalizeApiError(
                mutation.error,
                t(
                  "details.nodes.remove.error",
                  'Could not remove "{{thingName}}" from this group.',
                  { thingName },
                ),
              )
            : null
        }
      >
        <Button
          type="button"
          variant="ghost"
          size="sm"
          color="error"
          startIcon={<X className="h-4 w-4" />}
          fullWidth={false}
        >
          {t("common:actions.remove", "Remove")}
        </Button>
      </ConfirmationDialog>
    </div>
  );
}
