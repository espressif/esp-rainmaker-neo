/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { AnyRoute } from '@tanstack/react-router';
import { createRouter as createTanStackRouter, createRoute, redirect } from '@tanstack/react-router'
import { rootRoute } from './routes/__root'
import type { RouteConfig } from '../src/config/app-routes.config';
import { routesConfig } from '../src/config/app-routes.config'
import { requireAuth } from '../src/lib/auth'
import { PageLoader } from '@espressif/dashboard-ui-components/common'
import React from 'react'

/**
 * Converts a route path to a component import path
 * Must match the keys produced by import.meta.glob('../src/pages/** /*.tsx')
 * Convention:
 * - '/' -> '../src/pages/index.tsx'
 * - '/home' -> '../src/pages/home/home.tsx'
 * - '/home/agents' -> '../src/pages/home/agents/agents.tsx'
 */
function pathToComponentPath(path: string): string {
  if (path === '/') {
    return '../src/pages/index.tsx'
  }

  const segments = path.replace(/^\//, '').split('/').filter(Boolean)
  const lastSegment = segments[segments.length - 1]
  // Dynamic segment (e.g. $groupName) maps to a detail page component in its parent folder
  const dynamicSegmentMap: Record<string, string> = {
    '$groupName': '$groupName/group-details',
    '$thingName': '$thingName/thing-details',
    '$jobId': '$jobId/job-details',
  }
  const pathSegments = lastSegment.startsWith('$')
    ? [...segments.slice(0, -1), dynamicSegmentMap[lastSegment] ?? 'details']
    : [...segments, lastSegment]

  return `../src/pages/${pathSegments.join('/')}.tsx`
}

/**
 * Matches a path template (with `$paramName` tokens) against a real pathname
 * and extracts param values. Returns null when the shape doesn't match.
 */
function matchPathTemplate(
  template: string,
  pathname: string,
): Record<string, string> | null {
  const tSeg = template.split('/').filter(Boolean)
  const pSeg = pathname.split('/').filter(Boolean)
  if (tSeg.length !== pSeg.length) {return null}
  const params: Record<string, string> = {}
  for (let i = 0; i < tSeg.length; i++) {
    const token = tSeg[i]
    if (token.startsWith('$')) {
      params[token.slice(1)] = decodeURIComponent(pSeg[i])
    } else if (token !== pSeg[i]) {
      return null
    }
  }
  return params
}

/**
 * Creates a beforeLoad handler for auth check and redirect logic
 */
function createBeforeLoad(auth: boolean, redirectTo?: string, fullPath?: string) {
  if (!auth && !redirectTo) {
    return undefined
  }

  return async (context: { location: { pathname: string; href: string } }) => {
    if (auth) {
      await requireAuth(context)
    }

    if (redirectTo && fullPath) {
      if (fullPath.includes('$')) {
        const params = matchPathTemplate(fullPath, context.location.pathname)
        if (params) {
          throw redirect({ to: redirectTo, params } as unknown as Parameters<typeof redirect>[0])
        }
      } else if (context.location.pathname === fullPath) {
        throw redirect({ to: redirectTo })
      }
    }
  }
}

/**
 * Pre-compute lazy components for all paths
 */
function collectAllPaths(routes: RouteConfig[], parentPath: string = ''): string[] {
  const paths: string[] = []
  for (const route of routes) {
    const fullPath = parentPath + route.path
    paths.push(fullPath)
    if (route.subroutes) {
      paths.push(...collectAllPaths(route.subroutes, fullPath))
    }
  }
  return paths
}

type PageModule = { default: React.ComponentType }

// Vite statically analyzes this glob and creates separate chunks per page at build time
const pageModules = import.meta.glob<PageModule>('../src/pages/**/*.tsx')

const lazyComponents: Record<string, React.LazyExoticComponent<React.ComponentType>> = {}
for (const path of collectAllPaths(routesConfig)) {
  const componentPath = pathToComponentPath(path)
  const importFn = pageModules[componentPath]
  if (importFn) {
    lazyComponents[path] = React.lazy(() =>
      importFn().then((m) => ({ default: m.default }))
    )
  } else {
    console.warn(`No module found for route: ${path} (tried ${componentPath})`)
  }
}

/**
 * Recursively creates nested route tree
 * Each route's parent is determined by its position in the config hierarchy
 */
function createNestedRoutes(
  routes: RouteConfig[],
  parentRoute: AnyRoute,
  parentPath: string = '',
  parentAuth: boolean = false
): AnyRoute[] {
  return routes.map((routeConfig) => {
    const fullPath = parentPath + routeConfig.path
    const auth = routeConfig.auth ?? parentAuth
    const component = lazyComponents[fullPath]
    
    if (!component) {
      console.warn(`No component for path: ${fullPath}`)
    }

    // For child routes, use relative path (just the segment, not full path)
    // Root-level routes use full path
    const routePath = parentRoute === rootRoute ? fullPath : routeConfig.path

    const beforeLoad = createBeforeLoad(auth, routeConfig.redirectTo, fullPath)
    
    const route = createRoute({
      getParentRoute: () => parentRoute,
      path: routePath,
      component: component,
      ...(beforeLoad && { beforeLoad }),
    })

    // Recursively create child routes
    if (routeConfig.subroutes && routeConfig.subroutes.length > 0) {
      const childRoutes = createNestedRoutes(routeConfig.subroutes, route, fullPath, auth)
      route.addChildren(childRoutes)
    }

    return route
  })
}

// Build nested route tree
const routes = createNestedRoutes(routesConfig, rootRoute)
const routeTree = rootRoute.addChildren(routes)

export function createRouter() {
  return createTanStackRouter({
    routeTree,
    defaultPendingComponent: PageLoader,
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createRouter>
  }
}
