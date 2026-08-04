/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  Alert,
  PageContainer,
  ContentContainer,
} from "@espressif/dashboard-ui-components/components";
import PostDeploymentSections from "./_components/post-deployment-sections";

export default function PostDeployment() {
  const { t } = useTranslation("post-deployment");

  return (
    <PageContainer
      className="p-0 w-full"
      noGutters
      heading={
        <div className="">
          <Alert
            hideIcon
            type="info"
            color="secondary"
            variant="solid"
            className="rounded-none"
            description={t(
              "productionNote",
              "If you intend to go to production, you need to raise the limits for Email and SMS sending. Also, you would need to request production access for SES and SMS, so unverified identities can receive the signup code. Please contact AWS support.",
            )}
          />
        </div>
      }
    >
      <h1 className="sr-only">{t("heading", "Post-Deployment")}</h1>
      <ContentContainer maxWidth="xl" noGutters>
        <PostDeploymentSections />
      </ContentContainer>
    </PageContainer>
  );
}
