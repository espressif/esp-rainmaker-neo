/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { SidebarGroupConfig } from "@espressif/dashboard-ui-components/components";
import type { RouteConfig } from "@/config/app-routes.config";

type IndexedRoute = {
  fullPath: string;
  redirectTo?: string;
};

function normalizePath(path: string): string {
  if (path === "/") {
    return path;
  }

  return path.endsWith("/") ? path.slice(0, -1) : path;
}

function getParentPath(path: string): string {
  const normalizedPath = normalizePath(path);
  const segments = normalizedPath.split("/").filter(Boolean);

  if (segments.length <= 1) {
    return "/";
  }

  return `/${segments.slice(0, -1).join("/")}`;
}

function collectIndexedRoutes(
  routes: RouteConfig[],
  parentPath = "",
): IndexedRoute[] {
  const indexedRoutes: IndexedRoute[] = [];

  for (const route of routes) {
    const fullPath = normalizePath(`${parentPath}${route.path}`);
    indexedRoutes.push({
      fullPath,
      redirectTo: route.redirectTo ? normalizePath(route.redirectTo) : undefined,
    });

    if (!route.subroutes?.length) {
      continue;
    }

    indexedRoutes.push(...collectIndexedRoutes(route.subroutes, fullPath));
  }

  return indexedRoutes;
}

export function assertSidebarGroupRedirects(
  sidebarGroups: SidebarGroupConfig[],
  routes: RouteConfig[],
): void {
  const indexedRoutes = collectIndexedRoutes(routes);
  const routeMap = new Map(indexedRoutes.map((route) => [route.fullPath, route]));

  for (const group of sidebarGroups) {
    const firstItemPath = group.items[0]?.path;
    if (!firstItemPath) {
      continue;
    }

    const normalizedFirstItemPath = normalizePath(firstItemPath);
    const parentPath = getParentPath(normalizedFirstItemPath);
    const parentRoute = routeMap.get(parentPath);

    if (!parentRoute) {
      throw new Error(
        `[sidebar-redirect-contract] Missing route config for sidebar group "${group.id}" parent path "${parentPath}".`,
      );
    }

    if (parentRoute.redirectTo !== normalizedFirstItemPath) {
      throw new Error(
        `[sidebar-redirect-contract] Invalid redirect for sidebar group "${group.id}". Route "${parentPath}" must redirect to "${normalizedFirstItemPath}" but is "${parentRoute.redirectTo ?? "undefined"}".`,
      );
    }
  }
}
