/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Palette } from "lucide-react";
import {
  Badge,
  PageContainer,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { usePublishedBrand } from "@/api/branding";
import { DeploymentDetailsQr } from "@/components/deployment-details-qr";
import { useConfigStore } from "@/stores/config.store";
import { RainmakerBrandError } from "@/utils/rainmaker-brand/rainmaker-brand";
import BrandActions from "./_components/brand-actions";
import BrandPreview from "./_components/brand-preview";
import BrandingFormSheet from "./_components/branding-form-sheet";
import BrandingMainContent from "./_components/branding-main-content";
import { BRAND_CARD_PROPS } from "./branding.constants";

export default function Branding() {
  const { t } = useTranslation("branding");
  const { data: brand, isLoading, error } = usePublishedBrand();
  const serverUrl = useConfigStore((s) => s.config?.SERVER_URL);
  const [isReplaceOpen, setIsReplaceOpen] = useState(false);

  return (
    <PageContainer maxWidth="xl" noGutters>
      <div className="grid gap-5 py-5 lg:grid-cols-[minmax(0,1fr)_22rem] lg:items-start">
        <div className="flex min-w-0 flex-col gap-5">
          <SectionCard
            {...BRAND_CARD_PROPS}
            icon={<Palette className="h-5 w-5" aria-hidden />}
            primaryText={
              <span className="flex items-center gap-2">
                {t("heading", "Branding")}
                <Badge color="info">{t("beta", "Beta")}</Badge>
              </span>
            }
            secondaryText={t(
              "secondaryText",
              "Publish the app brand designed in ESP RainMaker Neo App Studio. Phones download it the next time ESP RainMaker Home opens and show it from the launch after.",
            )}
            truncateSecondaryText={false}
            actions={
              // A published brand this dashboard cannot read can still be replaced or deleted.
              (brand || error instanceof RainmakerBrandError) && (
                <BrandActions onReplace={() => setIsReplaceOpen(true)} />
              )
            }
          >
            <BrandingMainContent
              brand={brand}
              isLoading={isLoading}
              error={error}
            />
          </SectionCard>
          {brand && <BrandPreview brand={brand} />}
        </div>
        {serverUrl && (
          <div className="lg:sticky lg:top-5">
            <DeploymentDetailsQr url={serverUrl} />
          </div>
        )}
      </div>

      {isReplaceOpen && (
        <BrandingFormSheet onClose={() => setIsReplaceOpen(false)} />
      )}
    </PageContainer>
  );
}
