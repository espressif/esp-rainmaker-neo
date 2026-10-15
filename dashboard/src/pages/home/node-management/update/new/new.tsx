/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  ContentContainer,
  PageContainer,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { UpdateNodesForm } from "./_components/update-nodes-form";

function UpdateNew() {
  const { t } = useTranslation("update");

  return (
    <PageContainer
      noGutters
      goBackLinkData={{
        show: true,
        label: t("new.backToUpdateJobs", "Back to update jobs"),
        href: "/home/node-management/update",
        linkComponent: TanstackRouterLink,
      }}
    >
      <ContentContainer noGutters className="p-0">
        <ContentContainer
          noGutters
          maxWidth="md"
          className="flex flex-col items-center justify-center p-0"
        >
          <Typography variant="h2">
            {t("new.pageTitle", "Update nodes")}
          </Typography>
        </ContentContainer>
        <ContentContainer noGutters maxWidth="lg">
          <UpdateNodesForm />
        </ContentContainer>
      </ContentContainer>
    </PageContainer>
  );
}

export default UpdateNew;
