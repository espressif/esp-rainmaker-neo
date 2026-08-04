/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Trash2 } from "lucide-react";
import {
  Button,
  ConfirmationDialog,
} from "@espressif/dashboard-ui-components/components";
import { useUpdateNodeTags } from "@/api/node-tags";
import EditThingTagPopover from "../edit-thing-tag-popover";
import type { ManageThingTagsRowActionsProps } from "./manage-thing-tags-row-actions.props";

export default function ManageThingTagsRowActions({
  thingName,
  type,
  row,
}: ManageThingTagsRowActionsProps) {
  const { t } = useTranslation(["nodes", "common"]);
  const mutation = useUpdateNodeTags(thingName);

  const handleConfirmDelete = useCallback(async () => {
    await mutation.mutateAsync({ [type]: { [row.key]: null } });
  }, [mutation, type, row.key]);

  const handleCancelDelete = useCallback(() => {
    mutation.reset();
  }, [mutation]);

  return (
    <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
      <EditThingTagPopover
        thingName={thingName}
        type={type}
        tagKey={row.key}
        initialValue={row.value}
      />
      <ConfirmationDialog
        title={t("tags.delete.title", "Delete tag")}
        description={t(
          "tags.delete.description",
          'Are you sure you want to delete the tag "{{key}}"? This cannot be undone.',
          { key: row.key },
        )}
        confirmButtonText={t("common:actions.delete", "Delete")}
        cancelButtonText={t("common:actions.cancel", "Cancel")}
        onConfirm={handleConfirmDelete}
        onCancel={handleCancelDelete}
        isLoading={mutation.isPending}
        error={mutation.error?.message ?? null}
      >
        <Button
          type="button"
          variant="ghost"
          size="icon"
          color="error"
          fullWidth={false}
          aria-label={t("tags.deleteTag", "Delete tag")}
          tooltip={t("tags.deleteTag", "Delete tag")}
        >
          <Trash2 className="h-4 w-4" />
        </Button>
      </ConfirmationDialog>
    </div>
  );
}
