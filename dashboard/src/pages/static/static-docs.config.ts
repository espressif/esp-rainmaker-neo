/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react";
import { FileText, ShieldCheck } from "lucide-react";
import { STATIC_BASE_PATH } from "@/config/app-routes.config";
import type { SupportedLanguage } from "@/lib/constants";

export type StaticDocId = "terms-of-use" | "privacy-policy";

export type StaticDocConfig = {
  id: StaticDocId;
  /** Route path this document is served at. */
  path: string;
  /** Key in the `static` namespace. Titles are never hardcoded at call sites. */
  titleKey: string;
  /** English fallback used until the key is translated. */
  titleFallback: string;
  /**
   * Sidebar icon. A component reference, not a name — `SidebarItemConfig.icon` is typed
   * `ComponentType<{ className?: string }>`.
   */
  icon: ComponentType<{ className?: string }>;
  /**
   * Locale → markdown file, relative to this file so the keys line up with the `?raw`
   * glob in `static-docs.utils.ts`.
   *
   * Deliberately partial: a document that has not been translated yet is an expected
   * state, resolved by falling back to English rather than by failing.
   */
  files: Partial<Record<SupportedLanguage, string>>;
};

/**
 * Single source of truth for the public static documents. The sidebar, the content
 * resolver and the page titles all project from this list, so adding a document means
 * one entry here, one route entry, one page file and its markdown files.
 */
export const STATIC_DOCS: readonly StaticDocConfig[] = [
  {
    id: "terms-of-use",
    path: `${STATIC_BASE_PATH}/terms-of-use`,
    titleKey: "docs.termsOfUse.title",
    titleFallback: "Terms of Use",
    icon: FileText,
    files: {
      en: "./docs/en/terms-of-use.md",
      zh: "./docs/zh/terms-of-use.md",
    },
  },
  {
    id: "privacy-policy",
    path: `${STATIC_BASE_PATH}/privacy-policy`,
    titleKey: "docs.privacyPolicy.title",
    titleFallback: "Privacy Policy",
    icon: ShieldCheck,
    files: {
      en: "./docs/en/privacy-policy.md",
      zh: "./docs/zh/privacy-policy.md",
    },
  },
];

export function getStaticDocById(docId: StaticDocId): StaticDocConfig | null {
  return STATIC_DOCS.find((doc) => doc.id === docId) ?? null;
}
