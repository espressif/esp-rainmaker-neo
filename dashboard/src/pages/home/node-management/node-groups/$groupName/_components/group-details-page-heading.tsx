/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { InternalPageHeader } from "@/components/internal-page-header";
import { NodeGroupAvatar } from "@/components/avatars/node-group-avatar";
import { ResourceArnPopover } from "@/components/resource-arn-popover";
import { ResourceCreatedAt } from "@/components/resource-created-at";
import { ResourceHeadingDescription } from "@/components/resource-heading-description";
import { NodeGroupStatusBadge } from "@/components/node-group/node-group-status-badge";
import { isDynamicNodeGroup } from "@/config/node-group-type.config";
import { GroupTypeBadge } from "../../_components/group-type-badge";
import { ParentGroupsPopover } from "../../_components/parent-groups-popover";
import { DeleteGroupButton } from "./delete-group-button";
import GroupDetailsPageTabs, {
  type GroupDetailsTab,
} from "./group-details-page-tabs";

export interface NodeGroupDetailsData {
  thingGroupName: string;
  thingGroupArn: string;
  thingGroupDescription: string | null;
  creationDateMs: number | null;
  queryString: string | null;
  /** Raw AWS status, `null` for static groups. Kept loose so an unmapped value still renders. */
  status: string | null;
  parentGroupNames: string[];
}

interface GroupDetailsPageHeadingProps {
  data: NodeGroupDetailsData;
  activeTab: GroupDetailsTab;
  onTabChange: (value: GroupDetailsTab) => void;
}

export default function GroupDetailsPageHeading({
  data,
  activeTab,
  onTabChange,
}: GroupDetailsPageHeadingProps) {
  const { t } = useTranslation("node-groups");
  // AWS does not allow a dynamic group to be part of a hierarchy, so it can never have parents.
  const isDynamic = isDynamicNodeGroup(data.queryString);

  return (
    <InternalPageHeader
      resourceLabel={t("details.resourceLabel", "Node Group ID")}
      resourceId={data.thingGroupName}
      metaEnd={
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <ResourceCreatedAt ts={data.creationDateMs} />
          <GroupTypeBadge queryString={data.queryString} />
          {!isDynamic && (
            <ParentGroupsPopover parentGroupNames={data.parentGroupNames} />
          )}
        </div>
      }
      avatar={<NodeGroupAvatar size={60} />}
      heading={data.thingGroupName}
      description={
        <ResourceHeadingDescription
          badge={<NodeGroupStatusBadge status={data.status} />}
          description={data.thingGroupDescription}
        />
      }
      actions={
        <div className="flex items-center gap-2">
          <ResourceArnPopover arn={data.thingGroupArn} />
          <DeleteGroupButton groupName={data.thingGroupName} />
        </div>
      }
      footer={
        <GroupDetailsPageTabs activeTab={activeTab} onTabChange={onTabChange} />
      }
    />
  );
}
