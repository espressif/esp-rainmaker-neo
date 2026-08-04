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
import NodeDetailsPageHeading from "./_components/node-details-page-heading";
import type { NodeDetailsTab } from "./_components/node-details-page-tabs";
import { useThingDetails } from "./_hooks/use-thing-details";
import { useRouteParams } from "@/lib/navigation/use-route-params";

const TAB_ROUTES: readonly NodeDetailsTab[] = [
  "overview",
  "tags",
  "attributes",
  "ota-jobs",
];

function getActiveTab(pathname: string, thingName: string): NodeDetailsTab {
  const prefix = `/home/node-management/nodes/${thingName}/`;
  if (!pathname.startsWith(prefix)) {
    return "overview";
  }
  const segment = pathname.slice(prefix.length).split("/")[0] as NodeDetailsTab;
  return TAB_ROUTES.includes(segment) ? segment : "overview";
}

export default function ThingDetailsPage() {
  const { t } = useTranslation("nodes");
  const params = useRouteParams<{ thingName?: string }>();
  const thingName = params.thingName;
  const location = useLocation();
  const navigate = useNavigate();

  const { data, isPending, isError, isSuccess } = useThingDetails(thingName);

  const activeTab = useMemo(
    () => (thingName ? getActiveTab(location.pathname, thingName) : "overview"),
    [location.pathname, thingName],
  );

  const handleTabChange = (tab: NodeDetailsTab) => {
    if (!thingName) {
      return;
    }
    switch (tab) {
      case "overview":
        void navigate({
          to: "/home/node-management/nodes/$thingName/overview",
          params: { thingName },
        });
        break;
      case "tags":
        void navigate({
          to: "/home/node-management/nodes/$thingName/tags",
          params: { thingName },
        });
        break;
      case "attributes":
        void navigate({
          to: "/home/node-management/nodes/$thingName/attributes",
          params: { thingName },
        });
        break;
      case "ota-jobs":
        void navigate({
          to: "/home/node-management/nodes/$thingName/ota-jobs",
          params: { thingName },
        });
        break;
    }
  };

  const showError = isError || (isSuccess && data === null);

  if (!thingName || isPending) {
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
                void navigate({ to: "/home/node-management/nodes" })
              }
            >
              {t("details.backToNodesList", "Back to nodes list")}
            </Button>
          }
        >
          {t("details.errorTitle", "Sorry, we can't load this node's details")}
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
        label: t("details.backToNodes", "Back"),
        href: "/home/node-management/nodes",
        LinkComponent: TanstackRouterLink,
        className: "mx-5 mt-5",
      }}
      heading={
        data ? (
          <NodeDetailsPageHeading
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
