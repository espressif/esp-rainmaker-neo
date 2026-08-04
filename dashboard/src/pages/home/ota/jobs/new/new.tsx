/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useLocation } from "@tanstack/react-router";
import {
  ContentContainer,
  PageContainer,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { CreateOtaJobForm } from "./_components/create-ota-job-form";
import { parseCreateOtaJobSearch } from "./_schema/create-ota-job-search.schema";

function CreateOtaJobNew() {
  const { t } = useTranslation("ota-jobs");
  const location = useLocation();
  const search = useMemo(
    () => parseCreateOtaJobSearch(location.search),
    [location.search],
  );

  return (
    <PageContainer
      noGutters
      goBackLinkData={{
        show: true,
        label: t("createOtaJobPage.backToJobs", "Back to OTA Jobs"),
        href: "/home/ota/jobs",
        LinkComponent: TanstackRouterLink,
      }}
    >
      <ContentContainer noGutters className="p-0">
        <ContentContainer
          noGutters
          maxWidth="md"
          className="flex flex-col items-center justify-center p-0"
        >
          <Typography variant="h2">
            {t("createOtaJobPage.pageTitle", "Create OTA Job")}
          </Typography>
        </ContentContainer>
        <ContentContainer noGutters maxWidth="lg">
          <CreateOtaJobForm firmwareKey={search.firmware_key} />
        </ContentContainer>
      </ContentContainer>
    </PageContainer>
  );
}

export default CreateOtaJobNew;
