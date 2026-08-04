/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import { LayoutDashboard } from "lucide-react";
import type { SidebarNavEntry } from "@espressif/dashboard-ui-components/components";
import { STATIC_DOCS } from "./static-docs.config";

/**
 * Flat sidebar for the public static pages — top-level links only, no groups.
 *
 * Document entries are projected from `STATIC_DOCS` so the nav and the content registry
 * cannot disagree about which documents exist.
 *
 * `t` comes from the `static` namespace.
 */
export function getStaticSidebarConfig(t: TFunction): SidebarNavEntry[] {
  return [
    {
      // Auth-gated, so an anonymous visitor is sent to /login — which doubles as the
      // sign-in entry point from a legal page.
      id: "admin-dashboard",
      label: t("sidebar.adminDashboard", "Admin dashboard"),
      icon: LayoutDashboard,
      path: "/home",
    },
    ...STATIC_DOCS.map((doc) => ({
      id: doc.id,
      label: t(doc.titleKey, doc.titleFallback),
      icon: doc.icon,
      path: doc.path,
    })),
  ];
}
