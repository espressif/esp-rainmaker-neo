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
import { GenerateNodesForm } from "./_components/generate-nodes-form";

function GenerateTestNodes() {
  const { t } = useTranslation("generate");

  return (
    <PageContainer
      noGutters
      heading={
        <div className="flex flex-col gap-1 pb-1">
          <Typography variant="h2">
            {t("pageTitle", "Generate test nodes")}
          </Typography>
          <Typography variant="body2" className="text-muted-foreground">
            {t(
              "pageDescription",
              "Generate device credentials and factory partition binaries for a batch of test nodes - ready to flash and provision.",
            )}
          </Typography>
        </div>
      }
    >
      <ContentContainer noGutters maxWidth="xl">
        <GenerateNodesForm />
      </ContentContainer>
    </PageContainer>
  );
}

export default GenerateTestNodes;
