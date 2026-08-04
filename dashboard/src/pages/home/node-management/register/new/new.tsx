/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ContentContainer,
  PageContainer,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { useNodeRegistrationHandoffStore } from "@/stores/node-registration-handoff.store";
import { RegisterNodesForm } from "./_components/register-nodes-form";

function RegisterNew() {
  const { t } = useTranslation("register");

  // Capture any CSV handed off from the generate flow once, then clear it so a
  // later direct visit to this page starts with an empty upload field.
  const [initialCertificateFile] = useState(
    () => useNodeRegistrationHandoffStore.getState().pendingCsvFile,
  );
  const clearPendingCsvFile = useNodeRegistrationHandoffStore(
    (store) => store.clearPendingCsvFile,
  );
  useEffect(() => {
    if (initialCertificateFile) {
      clearPendingCsvFile();
    }
    // Run once on mount — the file is captured lazily above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <PageContainer
      noGutters
      goBackLinkData={{
        show: true,
        label: t(
          "new.backToRegistrationJobs",
          "Back to registration jobs",
        ),
        href: "/home/node-management/register",
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
            {t("new.pageTitle", "Register nodes")}
          </Typography>
        </ContentContainer>
        <ContentContainer noGutters maxWidth="lg">
          <RegisterNodesForm initialCertificateFile={initialCertificateFile ?? undefined} />
        </ContentContainer>
      </ContentContainer>
    </PageContainer>
  );
}

export default RegisterNew;
