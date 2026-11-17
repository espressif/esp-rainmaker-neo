/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useLocation } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ErrorBoundary } from "react-error-boundary";
import {
  Alert,
  ContentContainer,
  PageContainer,
} from "@espressif/dashboard-ui-components/components";
import { PageLoader } from "@espressif/dashboard-ui-components/common";
import type { TFunction } from "i18next";
import { findLockedCatalogEntry } from "@/enterprise/catalog";
import { LockedFeaturePage } from "./_components/locked-feature-page";
import {
  findEnterpriseRoute,
  useEnterpriseStore,
  type FeatureStatus,
} from "@/enterprise/enterprise.store";

function Banner({ message }: { message: string }) {
  return (
    <PageContainer maxWidth="xl" noGutters>
      <ContentContainer noGutters className="py-5">
        <Alert variant="soft" color="gray">
          {message}
        </Alert>
      </ContentContainer>
    </PageContainer>
  );
}

/** Why the feature at this path is not on screen, or null when it can render. */
function blockedMessage(t: TFunction, status: FeatureStatus, hasComponent: boolean): string | null {
  if (status === "not-deployed") {
    return t("enterprise.notFound", "This page is not available in this deployment.");
  }
  if (status === "incompatible") {
    return t(
      "enterprise.incompatible",
      "This enterprise feature requires a newer dashboard version and was not loaded.",
    );
  }
  if (status === "version-mismatch") {
    return t(
      "enterprise.versionMismatch",
      "This enterprise feature and its backend are from different releases. Update both to the same release to use it.",
    );
  }
  if (status === "failed" || !hasComponent) {
    return t("enterprise.unavailable", "This enterprise feature could not be loaded.");
  }
  return null;
}

/**
 * Renders whatever runtime-loaded enterprise plugin claims the current /home/ent/* path. Every plugin renders inside its own error boundary, so a broken feature degrades to a banner instead of taking the app down.
 */
export default function EnterpriseOutlet() {
  const { t } = useTranslation("common");
  const location = useLocation();
  const status = useEnterpriseStore((s) => s.status);
  const features = useEnterpriseStore((s) => s.features);

  if (status === "idle" || status === "loading") {
    return <PageLoader />;
  }

  // Checked first: a feature this deployment lacks, or whose backend is not deployed, is a locked catalog entry before it is anything else.
  const locked = findLockedCatalogEntry(status, features, location.pathname);
  if (locked) {
    return <LockedFeaturePage entry={locked} />;
  }

  const match = findEnterpriseRoute(features, location.pathname);
  if (!match) {
    // With the plugins themselves failing to load, an unknown path is a failure, not a missing page.
    const message =
      status === "error"
        ? t("enterprise.unavailable", "This enterprise feature could not be loaded.")
        : t("enterprise.notFound", "This page is not available in this deployment.");
    return <Banner message={message} />;
  }

  const Component = match.feature.components[match.route.componentId];
  const blocked = blockedMessage(t, match.feature.status, Boolean(Component));
  if (blocked) {
    return <Banner message={blocked} />;
  }
  return (
    <ErrorBoundary
      fallbackRender={() => (
        <Banner
          message={t(
            "enterprise.unavailable",
            "This enterprise feature could not be loaded.",
          )}
        />
      )}
    >
      <Component />
    </ErrorBoundary>
  );
}
