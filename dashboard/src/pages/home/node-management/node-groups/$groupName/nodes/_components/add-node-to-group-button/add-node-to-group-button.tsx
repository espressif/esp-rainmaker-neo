/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "lucide-react";
import { Button, toast } from "@espressif/dashboard-ui-components/components";
import { useAddThingToGroupMutation } from "@/api/node-groups";
import { normalizeApiError } from "@/lib/normalize-api-error";
import type { AddNodeToGroupButtonProps } from "./add-node-to-group-button.props";

export default function AddNodeToGroupButton({
  groupName,
  thingName,
}: AddNodeToGroupButtonProps) {
  const { t } = useTranslation(["node-groups", "common"]);
  const mutation = useAddThingToGroupMutation();

  // `mutateAsync`, not `mutate` with callbacks: the optimistic `onMutate` flips
  // this row to its "Remove" variant, unmounting this component before the
  // request settles. TanStack Query skips `mutate` callbacks once the observer
  // has no listeners, so the toasts would never fire. An awaited promise is
  // unaffected by the unmount.
  const handleAdd = useCallback(async () => {
    try {
      await mutation.mutateAsync({ groupName, thingName });
      toast.success({
        title: t(
          "details.nodes.add.success",
          'Node "{{thingName}}" added to this group.',
          { thingName },
        ),
        children: t(
          "common:toast.listRefreshNote",
          "The list updates automatically in a few seconds.",
        ),
      });
    } catch (error) {
      toast.error(
        normalizeApiError(
          error,
          t(
            "details.nodes.add.error",
            'Could not add "{{thingName}}" to this group.',
            { thingName },
          ),
        ),
      );
    }
  }, [mutation, groupName, thingName, t]);

  return (
    <Button
      type="button"
      variant="ghost"
      color="primary"
      size="sm"
      startIcon={<Plus className="h-4 w-4" />}
      onClick={() => void handleAdd()}
      disabled={mutation.isPending || !groupName}
      fullWidth={false}
    >
      {t("common:actions.add", "Add")}
    </Button>
  );
}
