/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { ArrowRightIcon } from "lucide-react";
import {
  AnimatedCard,
  Button,
  FullSizeError,
  SectionCard,
  ContentContainer,
} from "@espressif/dashboard-ui-components/components";
import { AppLogo } from "@espressif/dashboard-ui-components";
import { appConfig } from "@/lib/app-config";
import { useAppStore } from "@/stores/app.store";
import { presetFallbackLogoAssets } from "@/components/brand-logo";
import type { ExpiredCredentialsErrorProps } from "./expired-credentials-error.props";

/**
 * Terminal state for a session whose temporary AWS credentials can no longer be
 * fetched — in practice an expired or revoked token, where every authenticated
 * call keeps failing until the user signs in again.
 *
 * Deliberately says nothing about the underlying AWS error: it is never
 * actionable for the user, and the only way out is a fresh sign-in.
 *
 * Lives outside the `/home` shell so it is renderable on its own: the real path
 * to it needs a live session that has just gone stale, which is impractical to
 * reproduce while working on the design.
 */
export default function ExpiredCredentialsError({
  onBackToLogin,
}: ExpiredCredentialsErrorProps) {
  const { t } = useTranslation("common");
  /*
   * The screen renders outside `WorkspaceLayout`, so nothing upstream hands the
   * logo a theme — read it from the store the same way the layout does.
   */
  const darkMode = useAppStore((state) => state.darkMode);

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <ContentContainer maxWidth="xl" noGutters>
        <div className="flex justify-center items-center mb-4">
          <AppLogo
            name={appConfig.projectName}
            logo={presetFallbackLogoAssets}
            darkMode={darkMode}
          />
        </div>
        <SectionCard variant="soft" color="error" allowCollapse={false}>
          <ContentContainer maxWidth="sm" noGutters>
            <FullSizeError
              title={t("expiredCredentials.title", "Your session has expired")}
              illustration={
                <AnimatedCard type="errorSpreadOut" iconSize={160} />
              }
            >
              <div className="flex flex-col items-center gap-6 text-center">
                <p>
                  {t(
                    "expiredCredentials.message",
                    "For your security you've been signed out because your session is no longer valid. Sign in again to pick up where you left off.",
                  )}
                </p>
                <Button
                  onClick={onBackToLogin}
                  endIcon={<ArrowRightIcon className="h-4 w-4" />}
                >
                  {t("expiredCredentials.action", "Back to login")}
                </Button>
              </div>
            </FullSizeError>
          </ContentContainer>
        </SectionCard>
      </ContentContainer>
    </div>
  );
}
