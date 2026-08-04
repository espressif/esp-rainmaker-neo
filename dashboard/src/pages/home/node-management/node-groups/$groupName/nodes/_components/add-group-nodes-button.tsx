/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Plus } from "lucide-react";
import { Button } from "@espressif/dashboard-ui-components/components";
import { TableRowDetailSheet } from "@/components/table-row-detail-sheet";
import { AwsThingsList } from "@/aws/components/aws-things-list";
import { useGroupThingNamesSetQuery } from "@/api/node-groups";
import GroupNodesRowActions from "./group-nodes-row-actions/group-nodes-row-actions";
import { AddNodeToGroupButton } from "./add-node-to-group-button";
import { useRouteParams } from "@/lib/navigation/use-route-params";

export default function AddGroupNodesButton() {
  const { t } = useTranslation("node-groups");
  const [open, setOpen] = useState(false);
  const params = useRouteParams<{ groupName?: string }>();
  const groupName = params.groupName ?? "";

  const { set: thingsInGroup } = useGroupThingNamesSetQuery(groupName);

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        color="primary"
        size="sm"
        startIcon={<Plus className="h-4 w-4" />}
        onClick={() => setOpen(true)}
        disabled={!groupName}
      >
        {t("details.nodes.addNodes", "Add nodes to this group")}
      </Button>
      {open && (
        <TableRowDetailSheet
          label={t(
            "details.nodes.addNodes",
            "Add nodes to this group",
          )}
          onOpenChange={(next) => {
            if (!next) {
              setOpen(false);
            }
          }}
        >
          <AwsThingsList
            actions={(row) =>
              thingsInGroup.has(row.thingName) ? (
                <GroupNodesRowActions
                  groupName={groupName}
                  thingName={row.thingName}
                />
              ) : (
                <AddNodeToGroupButton
                  groupName={groupName}
                  thingName={row.thingName}
                />
              )
            }
          />
        </TableRowDetailSheet>
      )}
    </>
  );
}
