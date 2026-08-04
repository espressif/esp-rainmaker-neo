/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { GroupsCard } from "@/components/groups-card";
import DeviceTypeCard from "../_components/device-type-card";
import OverviewTagsSection from "../_components/overview-tags-section";
import { useThingDetails } from "../_hooks/use-thing-details";
import { useRouteParams } from "@/lib/navigation/use-route-params";

export default function ThingOverviewPage() {
  const { t } = useTranslation("nodes");
  const params = useRouteParams<{ thingName?: string }>();
  const thingName = params.thingName;
  const { data } = useThingDetails(thingName);

  if (!thingName || !data) {
    return null;
  }

  const hasDeviceInfo = Boolean(data.type || data.model || data.fwVersion);

  return (
    <div className="flex w-full">
      <div className="mx-auto flex w-full max-w-xl flex-col gap-5">
        {hasDeviceInfo ? (
          <DeviceTypeCard
            type={data.type}
            model={data.model}
            version={data.fwVersion}
          />
        ) : null}
        <GroupsCard
          groupNames={data.thingGroupNames}
          primaryText={t("details.overview.groups", "Groups")}
          emptyText={t("details.overview.noGroups", "No groups")}
        />
        <OverviewTagsSection thingName={thingName} />
      </div>
    </div>
  );
}
