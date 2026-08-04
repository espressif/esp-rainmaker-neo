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
import { UploadOtaImageForm } from "./_components/upload-ota-image-form";

function UploadOtaImageNew() {
  const { t } = useTranslation("ota-images");

  return (
    <PageContainer
      noGutters
      goBackLinkData={{
        show: true,
        label: t("backToImages", "Back to OTA Images"),
        href: "/home/ota/images",
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
            {t("pageTitle", "Upload OTA Image")}
          </Typography>
        </ContentContainer>
        <ContentContainer noGutters maxWidth="lg">
          <UploadOtaImageForm />
        </ContentContainer>
      </ContentContainer>
    </PageContainer>
  );
}

export default UploadOtaImageNew;
