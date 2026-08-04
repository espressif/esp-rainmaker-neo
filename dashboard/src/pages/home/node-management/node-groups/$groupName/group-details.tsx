/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  AnimatedCard,
  Button,
  PageContainer,
  ProgressBar,
} from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { useNodeGroupDetailsQuery } from "@/api/node-groups";
import GroupDetailsPageHeading, {
  type NodeGroupDetailsData,
} from "./_components/group-details-page-heading";
import type { GroupDetailsTab } from "./_components/group-details-page-tabs";
import { useRouteParams } from "@/lib/navigation/use-route-params";

const TAB_ROUTES: readonly GroupDetailsTab[] = ["nodes", "ota-jobs"];

function getActiveTab(pathname: string, groupName: string): GroupDetailsTab {
  const prefix = `/home/node-management/node-groups/${groupName}/`;
  if (!pathname.startsWith(prefix)) {
    return "nodes";
  }
  const segment = pathname.slice(prefix.length).split("/")[0] as GroupDetailsTab;
  return TAB_ROUTES.includes(segment) ? segment : "nodes";
}

function toMillis(value: number | Date | undefined): number | null {
  if (value instanceof Date) {
    return value.getTime();
  }
  if (typeof value === "number") {
    // AWS returns creation date as seconds since epoch.
    return value * 1000;
  }
  return null;
}

export default function GroupDetailsPage() {
  const { t } = useTranslation("node-groups");
  const params = useRouteParams<{ groupName?: string }>();
  const groupName = params.groupName;
  const location = useLocation();
  const navigate = useNavigate();

  const { data: response, isPending, isError, isSuccess } =
    useNodeGroupDetailsQuery(groupName);

  const data = useMemo<NodeGroupDetailsData | null>(() => {
    if (!response) {
      return null;
    }
    return {
      thingGroupName: response.thingGroupName ?? groupName ?? "",
      thingGroupArn: response.thingGroupArn ?? "",
      thingGroupDescription:
        response.thingGroupProperties?.thingGroupDescription ?? null,
      creationDateMs: toMillis(response.thingGroupMetadata?.creationDate),
      queryString: response.queryString ?? null,
      status: response.status ?? null,
      parentGroupNames:
        response.thingGroupMetadata?.rootToParentThingGroups
          ?.map((entry) => entry.groupName ?? "")
          .filter((name): name is string => Boolean(name)) ?? [],
    };
  }, [response, groupName]);

  const activeTab = useMemo(
    () => (groupName ? getActiveTab(location.pathname, groupName) : "nodes"),
    [location.pathname, groupName],
  );

  const handleTabChange = (tab: GroupDetailsTab) => {
    if (!groupName) {
      return;
    }
    switch (tab) {
      case "nodes":
        void navigate({
          to: "/home/node-management/node-groups/$groupName/nodes",
          params: { groupName },
        });
        break;
      case "ota-jobs":
        void navigate({
          to: "/home/node-management/node-groups/$groupName/ota-jobs",
          params: { groupName },
        });
        break;
    }
  };

  const showError = isError || (isSuccess && data === null);

  if (!groupName || isPending) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center px-5">
        <ProgressBar showFakeProgress className="w-full max-w-sm" />
      </div>
    );
  }

  if (showError) {
    return (
      <div className="mx-auto flex min-h-[50vh] max-w-lg items-center justify-center p-6">
        <AnimatedCard
          type="errorSpreadOut"
          iconSize={160}
          actions={
            <Button
              variant="ghost"
              color="primary"
              onClick={() =>
                void navigate({ to: "/home/node-management/node-groups" })
              }
            >
              {t("details.backToGroupsList", "Back to node groups list")}
            </Button>
          }
        >
          {t(
            "details.errorTitle",
            "Sorry, we can't load this node group's details",
          )}
        </AnimatedCard>
      </div>
    );
  }

  return (
    <PageContainer
      noGutters
      className="p-0"
      elevateHeading
      goBackLinkData={{
        show: true,
        label: t("details.backToGroups", "Back to node groups list"),
        href: "/home/node-management/node-groups",
        LinkComponent: TanstackRouterLink,
        className: "mx-5 mt-5",
      }}
      heading={
        data ? (
          <GroupDetailsPageHeading
            data={data}
            activeTab={activeTab}
            onTabChange={handleTabChange}
          />
        ) : null
      }
    >
      <div className="px-5 pb-5">
        <Outlet />
      </div>
    </PageContainer>
  );
}
