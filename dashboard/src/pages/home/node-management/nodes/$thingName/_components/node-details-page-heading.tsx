/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Badge,
  SimplifiedDate,
} from "@espressif/dashboard-ui-components/components";
import { InternalPageHeader } from "@/components/internal-page-header";
import { ResourceArnPopover } from "@/components/resource-arn-popover";
import { ThingDeviceAvatar } from "@/components/avatars/thing-device-avatar";
import { ThingStatusBadge } from "@/components/thing/thing-status-badge";
import { getThingDisplayStatus } from "@/config/node-status.config";
import type { ThingDetailsData } from "../_hooks/use-thing-details";
import NodeDetailsPageTabs, {
  type NodeDetailsTab,
} from "./node-details-page-tabs";

interface NodeDetailsPageHeadingProps {
  data: ThingDetailsData;
  activeTab: NodeDetailsTab;
  onTabChange: (value: NodeDetailsTab) => void;
}

export default function NodeDetailsPageHeading({
  data,
  activeTab,
  onTabChange,
}: NodeDetailsPageHeadingProps) {
  const { t } = useTranslation("nodes");
  const status = getThingDisplayStatus(data.online);

  return (
    <InternalPageHeader
      resourceLabel={t("details.thingNameLabel", "Thing name")}
      resourceId={data.thingName}
      metaEnd={
        data.fwVersion ? (
          <Badge
            variant="outline"
            className="text-xs font-normal"
            color="secondary"
          >
            {t("details.firmwareVersion", "Firmware Version")} {data.fwVersion}
          </Badge>
        ) : null
      }
      avatar={
        <ThingDeviceAvatar
          deviceType={data.type ?? null}
          online={data.online}
          size={56}
        />
      }
      heading={data.displayName}
      actions={<ResourceArnPopover arn={data.thingArn} />}
      description={
        status ? (
          <span className="inline-flex items-center gap-1.5">
            <ThingStatusBadge online={data.online} />
            {data.lastStatusTs && data.lastStatusTs > 0 ? (
              <span className="text-muted-foreground">
                ({t("details.statusUpdatedPrefix", "updated")}{" "}
                <SimplifiedDate
                  ts={data.lastStatusTs}
                  relative
                  className="text-muted-foreground"
                />
                )
              </span>
            ) : null}
          </span>
        ) : null
      }
      footer={
        <NodeDetailsPageTabs activeTab={activeTab} onTabChange={onTabChange} />
      }
    />
  );
}
