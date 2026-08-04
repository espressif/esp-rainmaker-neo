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
import { CreateNodeGroupForm } from "./_components/create-node-group-form";

function CreateNodeGroupNew() {
  const { t } = useTranslation("node-groups");

  return (
    <PageContainer
      noGutters
      goBackLinkData={{
        show: true,
        label: t("new.backToGroups", "Back to node groups"),
        href: "/home/node-management/node-groups",
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
            {t("new.pageTitle", "Create node group")}
          </Typography>
        </ContentContainer>
        <ContentContainer noGutters maxWidth="lg">
          <CreateNodeGroupForm />
        </ContentContainer>
      </ContentContainer>
    </PageContainer>
  );
}

export default CreateNodeGroupNew;
