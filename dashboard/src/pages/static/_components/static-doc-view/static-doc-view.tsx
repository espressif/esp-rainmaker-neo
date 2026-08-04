/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  FullSizeError,
  MarkdownContent,
  PageContainer,
} from "@espressif/dashboard-ui-components/components";
import { TanstackRouterLink } from "@/lib/navigation/router-link-adapters";
import { useAppStore } from "@/stores/app.store";
import { resolveStaticDoc } from "../../static-docs.utils";
import type { StaticDocViewProps } from "./static-doc-view.props";

/**
 * Renders one static document, resolved from the registry for the active locale.
 *
 * This is the single app-state boundary for static pages: it owns the store reads, the
 * translation namespace, the router link adapter and the registry lookup, then hands the
 * library's `MarkdownContent` plain values — that renderer takes no app state of its own.
 *
 * The document supplies its own top-level heading, so no page header is rendered here.
 */
export default function StaticDocView({ docId }: StaticDocViewProps) {
  const { t } = useTranslation("static");
  const language = useAppStore((state) => state.language);
  const darkMode = useAppStore((state) => state.darkMode);

  const doc = useMemo(() => resolveStaticDoc(docId, language), [docId, language]);

  if (!doc) {
    return (
      <FullSizeError title={t("unavailable.title", "Content not available")}>
        {t(
          "unavailable.message",
          "This document is not available yet. Please check back later.",
        )}
      </FullSizeError>
    );
  }

  return (
    <PageContainer maxWidth="xl" noGutters>
      <div className="py-5">
        <MarkdownContent
          content={doc.content}
          linkComponent={TanstackRouterLink}
          colorTheme={darkMode ? "dark" : "light"}
        />
      </div>
    </PageContainer>
  );
}
