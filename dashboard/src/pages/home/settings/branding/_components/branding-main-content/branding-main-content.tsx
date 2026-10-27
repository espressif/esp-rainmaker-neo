/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useTranslation } from "react-i18next";
import { Moon, Sun, SunMoon } from "lucide-react";
import {
  Alert,
  Badge,
  SimplifiedDate,
  Skeleton,
  Typography,
} from "@espressif/dashboard-ui-components/components";
import { getBrandingErrorMessage } from "../../branding.utils";
import BrandingForm from "../branding-form";
import type { BrandingMainContentProps } from "./branding-main-content.props";

const APPEARANCE_ICONS = { light: Sun, dark: Moon, both: SunMoon } as const;

export default function BrandingMainContent({
  brand,
  isLoading,
  error,
}: BrandingMainContentProps) {
  const { t } = useTranslation("branding");

  if (isLoading) {
    return (
      <div className="flex flex-col gap-2">
        <Skeleton className="h-7 w-1/3" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-4 w-1/4" />
      </div>
    );
  }

  if (error) {
    return (
      <Alert
        type="error"
        variant="outline"
        title={t("loadError", "Could not load the published brand")}
        description={getBrandingErrorMessage(
          error,
          t,
          t("loadErrorDescription", "Reload the page to try again."),
        )}
      />
    );
  }

  if (!brand) {
    return (
      <div className="flex flex-col gap-6">
        <Alert type="info" variant="soft" color="gray">
          {t(
            "empty",
            "No brand is published, so ESP RainMaker Home shows its default look on this deployment.",
          )}
        </Alert>
        <BrandingForm />
      </div>
    );
  }

  const { config } = brand;
  const AppearanceIcon = APPEARANCE_ICONS[brand.appearance];
  const appearanceLabel = {
    light: t("summary.appearance.light", "Light mode only"),
    dark: t("summary.appearance.dark", "Dark mode only"),
    both: t("summary.appearance.both", "Light and dark mode"),
  }[brand.appearance];

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Typography variant="h3" className="truncate">
        {config.appName}
      </Typography>
      {config.tagline && (
        <Typography variant="body1" className="text-muted-foreground">
          {config.tagline}
        </Typography>
      )}
      {config.company && (
        <p className="text-sm text-muted-foreground">{config.company}</p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2">
        <Badge variant="soft" color="primary" className="gap-1.5 font-normal">
          <AppearanceIcon className="h-3.5 w-3.5 shrink-0" aria-hidden />
          {appearanceLabel}
        </Badge>
        <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
          {t("summary.published", "Published")}
          <SimplifiedDate
            ts={Date.parse(brand.publishedAt)}
            relative
            className="text-muted-foreground"
          />
        </span>
      </div>
    </div>
  );
}
