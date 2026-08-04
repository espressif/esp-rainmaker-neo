/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { TFunction } from "i18next";
import type { BreadcrumbItemConfig } from "@espressif/dashboard-ui-components/layouts";
import type { RouteConfig } from "@/config/app-routes.config";

type Match = {
  route: RouteConfig;
  fullPath: string;
  depth: number;
};

/**
 * Returns true when `template` is a segment-by-segment prefix of `pathname`.
 * Tokens that start with `$` (TanStack Router param convention) match any
 * single non-empty segment. The literal splat token `$` therefore matches a
 * single tail segment — not multiple. This is sufficient for breadcrumb
 * derivation: deeper splat tails contribute no static crumbs anyway.
 */
function templateMatchesPrefix(template: string, pathname: string): boolean {
  const tSeg = template.split("/").filter(Boolean);
  const pSeg = pathname.split("/").filter(Boolean);
  if (tSeg.length > pSeg.length) {
    return false;
  }
  for (let i = 0; i < tSeg.length; i++) {
    const tok = tSeg[i];
    if (tok.startsWith("$")) {
      continue;
    }
    if (tok !== pSeg[i]) {
      return false;
    }
  }
  return true;
}

function collectMatches(
  routes: RouteConfig[],
  pathname: string,
  parentPath: string,
  out: Match[],
): void {
  for (const r of routes) {
    const full = parentPath + r.path;
    if (!templateMatchesPrefix(full, pathname)) {
      continue;
    }
    out.push({
      route: r,
      fullPath: full,
      depth: full.split("/").filter(Boolean).length,
    });
    if (r.subroutes) {
      collectMatches(r.subroutes, pathname, full, out);
    }
  }
}

/**
 * Walks `routes` against `pathname` and emits the chain of crumbs derived
 * from each matching route's optional `breadcrumb` metadata. Routes without
 * `breadcrumb` are skipped — that is the intended way to drop dynamic
 * segments (`$userId`, `$roleName`, …) and pure-redirect parents.
 *
 * The link target for each crumb is taken from the **actual** pathname
 * (sliced to the matching template's segment count) so dynamic params don't
 * need separate substitution and links never contain raw `$param` tokens.
 *
 * The last (current) crumb has its `path` cleared so the layout renders it
 * as a non-link page indicator rather than a hyperlink.
 */
export function buildBreadcrumbs(
  routes: RouteConfig[],
  pathname: string,
  t: TFunction,
): BreadcrumbItemConfig[] {
  const matches: Match[] = [];
  collectMatches(routes, pathname, "", matches);
  matches.sort((a, b) => a.depth - b.depth);

  const segs = pathname.split("/").filter(Boolean);
  const annotated = matches.filter((m) => m.route.breadcrumb);

  return annotated.map((m, idx) => {
    const isLast = idx === annotated.length - 1;
    const concretePath = "/" + segs.slice(0, m.depth).join("/");
    const breadcrumb = m.route.breadcrumb;
    if (!breadcrumb) {
      return { label: "", path: isLast ? undefined : concretePath };
    }
    const { i18nKey, fallback } = breadcrumb;
    return {
      label: i18nKey ? t(i18nKey, fallback) : fallback,
      path: isLast ? undefined : concretePath,
    };
  });
}
