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
import {
  findEnterpriseRoute,
  useEnterpriseStore,
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

/**
 * Renders whatever runtime-loaded enterprise plugin claims the current /home/ent/* path. Every
 * plugin renders inside its own error boundary, so a broken feature degrades to a banner instead
 * of taking the app down.
 */
export default function EnterpriseOutlet() {
  const { t } = useTranslation("common");
  const location = useLocation();
  const status = useEnterpriseStore((s) => s.status);
  const features = useEnterpriseStore((s) => s.features);

  if (status === "idle" || status === "loading") {
    return <PageLoader />;
  }

  const match = findEnterpriseRoute(features, location.pathname);
  if (!match) {
    return (
      <Banner
        message={t(
          "enterprise.notFound",
          "This page is not available in this deployment.",
        )}
      />
    );
  }
  if (match.feature.status === "incompatible") {
    return (
      <Banner
        message={t(
          "enterprise.incompatible",
          "This enterprise feature requires a newer dashboard version and was not loaded.",
        )}
      />
    );
  }
  const Component = match.feature.components[match.route.componentId];
  if (match.feature.status !== "loaded" || !Component) {
    return (
      <Banner
        message={t(
          "enterprise.unavailable",
          "This enterprise feature could not be loaded.",
        )}
      />
    );
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
