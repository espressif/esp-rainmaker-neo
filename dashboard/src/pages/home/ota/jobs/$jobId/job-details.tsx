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
import { useOtaJobDetailsQuery } from "@/api/ota-jobs";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import OtaJobDetailsPageHeading from "./_components/ota-job-details-page-heading";
import type { OtaJobDetailsTab } from "./_components/ota-job-details-page-tabs";
import { useRouteParams } from "@/lib/navigation/use-route-params";

const TAB_ROUTES: readonly OtaJobDetailsTab[] = ["overview", "nodes"];

function getActiveTab(pathname: string, jobId: string): OtaJobDetailsTab {
  const prefix = `/home/ota/jobs/${jobId}/`;
  if (!pathname.startsWith(prefix)) {
    return "overview";
  }
  const segment = pathname.slice(prefix.length).split("/")[0] as OtaJobDetailsTab;
  return TAB_ROUTES.includes(segment) ? segment : "overview";
}

export default function OtaJobDetailsPage() {
  const { t } = useTranslation("ota-jobs");
  const params = useRouteParams<{ jobId?: string }>();
  const jobId = params.jobId;
  const location = useLocation();
  const navigate = useNavigate();

  const { data, isPending, isError, isSuccess } = useOtaJobDetailsQuery(jobId);

  const activeTab = useMemo(
    () => (jobId ? getActiveTab(location.pathname, jobId) : "overview"),
    [location.pathname, jobId],
  );

  const handleTabChange = (tab: OtaJobDetailsTab) => {
    if (!jobId) {
      return;
    }
    switch (tab) {
      case "overview":
        void navigate({
          to: "/home/ota/jobs/$jobId/overview",
          params: { jobId },
        });
        break;
      case "nodes":
        void navigate({
          to: "/home/ota/jobs/$jobId/nodes",
          params: { jobId },
        });
        break;
    }
  };

  const showError = isError || (isSuccess && data === null);

  if (!jobId || isPending) {
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
              onClick={() => void navigate({ to: "/home/ota/jobs" })}
            >
              {t("details.backToJobsList", "Back to OTA Jobs list")}
            </Button>
          }
        >
          {t("details.errorTitle", "Sorry, we can't load this OTA job's details")}
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
        label: t("details.backToJobs", "Back to OTA Jobs"),
        href: "/home/ota/jobs",
        LinkComponent: TanstackRouterLink,
        className: "mx-5 mt-5",
      }}
      heading={
        data ? (
          <OtaJobDetailsPageHeading
            job={data}
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
