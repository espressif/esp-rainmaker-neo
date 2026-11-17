/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import {
  ContentContainer,
  PageContainer,
  SectionCard,
} from "@espressif/dashboard-ui-components/components";
import { Lock } from "lucide-react";
import { resolveEnterpriseIcon } from "@/enterprise/enterprise-icons";
import type { LockedFeaturePageProps } from "./locked-feature-page.props";

/**
 * The one page behind a locked sidebar item: what the feature does and how to get it. Static on purpose, with nothing that prompts or repeats.
 */
export default function LockedFeaturePage({ entry }: LockedFeaturePageProps) {
  const { t } = useTranslation("common");
  const Icon = resolveEnterpriseIcon(entry.icon);

  return (
    <PageContainer maxWidth="xl" noGutters>
      <ContentContainer noGutters className="py-5">
        <SectionCard
          allowCollapse={false}
          icon={<Icon className="h-6 w-6" />}
          primaryText={t(entry.labelKey, entry.labelFallback)}
          color="silver"
          variant="outline"
        >
          <div className="flex flex-col gap-4 text-sm">
            <p>{t(entry.descriptionKey, entry.descriptionFallback)}</p>
            <div className="flex items-start gap-3">
              <Lock className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
              <p>
                {t(
                  "enterprise.lockedPage.howToGet",
                  "This feature is part of the ESP RainMaker Neo Enterprise plan and is not installed on this deployment. Contact the ESP RainMaker team to add it.",
                )}
              </p>
            </div>
          </div>
        </SectionCard>
      </ContentContainer>
    </PageContainer>
  );
}
