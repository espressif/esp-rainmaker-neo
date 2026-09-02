/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  ACCOUNT_SETTINGS_ROUTE_SEGMENT,
  ACCOUNT_SETTINGS_TABS,
  DEFAULT_ACCOUNT_SETTINGS_TAB,
} from "./account-settings.config";

export type RouteBreadcrumb = {
  /**
   * Fully-qualified i18next key (e.g. 'nodes:breadcrumbs.overview').
   * Bare keys without a `<ns>:` prefix resolve against the `common` namespace.
   */
  i18nKey?: string;
  /** English fallback used when the i18n key is missing or unset. */
  fallback: string;
};

export type RouteConfig = {
  path: string;
  auth?: boolean;
  /**
   * Exempts this route's subtree from the runtime-config gate in `AppBootstrap`.
   * Set only on routes that render purely from bundled assets and must stay
   * reachable when the backend is unreachable (legal documents under `/static`).
   */
  skipBootstrap?: boolean;
  redirectTo?: string;
  subroutes?: RouteConfig[];
  /**
   * Optional breadcrumb metadata. Routes without this field are not rendered
   * as a crumb — the natural place to opt out for dynamic segments
   * (`$thingName`, `$groupName`, …) and pure-redirect parents (`/node-management`,
   * `/ota`, sidebar group paths, …).
   */
  breadcrumb?: RouteBreadcrumb;
};

/**
 * Base path of the public, unauthenticated static-document pages.
 *
 * Declared here — not in the page's own config — because `routesConfig` is imported by
 * `app/router.tsx` at module load. A route-config import reaching into `src/pages/static/`
 * would drag the document registry and its eager `?raw` markdown glob into the main
 * bundle instead of the lazily-loaded `/static` chunk.
 */
export const STATIC_BASE_PATH = "/static";

/**
 * Account settings tabs are declared once in `account-settings.config.ts`; their routes
 * are projected from that list so paths and labels cannot drift apart. Breadcrumb labels
 * reuse the tab labels rather than duplicating a parallel set of i18n keys.
 */
const accountSettingsSubroutes: RouteConfig[] = ACCOUNT_SETTINGS_TABS.map(
  (tab) => ({
    path: `/${tab.id}`,
    breadcrumb: {
      i18nKey: `account-settings:${tab.labelKey}`,
      fallback: tab.fallback,
    },
  }),
);

export const routesConfig: RouteConfig[] = [
  // Public routes - no authentication required
  { path: "/", redirectTo: "/login" },
  { path: "/error" },

  // Auth routes. The login steps are flat routes, not `subroutes`: `/login` is
  // itself the remembered-account screen, not a layout with an <Outlet/>.
  { path: "/login" },
  { path: "/login/email" },
  { path: "/login/otp" },
  { path: "/login/password" },
  { path: "/forgot-password" },
  { path: "/set-password" },
  { path: "/logout" },

  /*
   * Landing point of the "Preview sign-in page" action. Unauthenticated on purpose: it is
   * reached mid-OAuth-redirect in a new tab, which may carry no dashboard session.
   */
  { path: "/oauth-preview" },

  // Public static documents - readable without an account or a reachable backend
  {
    path: STATIC_BASE_PATH,
    redirectTo: `${STATIC_BASE_PATH}/terms-of-use`,
    skipBootstrap: true,
    subroutes: [
      {
        path: "/terms-of-use",
        breadcrumb: {
          i18nKey: "static:docs.termsOfUse.title",
          fallback: "Terms of Use",
        },
      },
      {
        path: "/privacy-policy",
        breadcrumb: {
          i18nKey: "static:docs.privacyPolicy.title",
          fallback: "Privacy Policy",
        },
      },
    ],
  },

  // Protected routes - require authentication
  {
    path: "/home",
    auth: true,
    redirectTo: "/home/node-management/nodes",
    breadcrumb: { i18nKey: "common:breadcrumbs.home", fallback: "Home" },
    subroutes: [
      {
        path: "/node-management",
        redirectTo: "/home/node-management/nodes",
        subroutes: [
          {
            path: "/nodes",
            breadcrumb: {
              i18nKey: "common:sidebar.nodes",
              fallback: "Nodes",
            },
            subroutes: [
              {
                path: "/$thingName",
                redirectTo:
                  "/home/node-management/nodes/$thingName/overview",
                subroutes: [
                  {
                    path: "/overview",
                    breadcrumb: {
                      i18nKey: "nodes:breadcrumbs.overview",
                      fallback: "Overview",
                    },
                  },
                  {
                    path: "/tags",
                    breadcrumb: {
                      i18nKey: "nodes:breadcrumbs.tags",
                      fallback: "Tags",
                    },
                  },
                  {
                    path: "/attributes",
                    breadcrumb: {
                      i18nKey: "nodes:breadcrumbs.attributes",
                      fallback: "Attributes",
                    },
                  },
                  {
                    path: "/ota-jobs",
                    breadcrumb: {
                      i18nKey: "nodes:breadcrumbs.otaJobs",
                      fallback: "OTA Jobs",
                    },
                  },
                ],
              },
            ],
          },
          {
            path: "/node-groups",
            breadcrumb: {
              i18nKey: "common:sidebar.nodeGroups",
              fallback: "Node groups",
            },
            subroutes: [
              {
                path: "/new",
                breadcrumb: {
                  i18nKey: "node-groups:breadcrumbs.new",
                  fallback: "Create new group",
                },
              },
              {
                path: "/$groupName",
                redirectTo:
                  "/home/node-management/node-groups/$groupName/nodes",
                subroutes: [
                  {
                    path: "/nodes",
                    breadcrumb: {
                      i18nKey: "node-groups:breadcrumbs.nodes",
                      fallback: "Nodes",
                    },
                  },
                  {
                    path: "/ota-jobs",
                    breadcrumb: {
                      i18nKey: "node-groups:breadcrumbs.otaJobs",
                      fallback: "OTA Jobs",
                    },
                  },
                ],
              },
            ],
          },
          {
            path: "/register",
            breadcrumb: {
              i18nKey: "register:breadcrumbs.jobs",
              fallback: "Registration jobs",
            },
            subroutes: [
              {
                path: "/new",
                breadcrumb: {
                  i18nKey: "register:breadcrumbs.new",
                  fallback: "Register nodes",
                },
              },
            ],
          },
          {
            path: "/generate",
            breadcrumb: {
              i18nKey: "common:sidebar.generateNodes",
              fallback: "Generate nodes",
            },
          },
        ],
      },
      {
        path: "/ota",
        redirectTo: "/home/ota/images",
        subroutes: [
          {
            path: "/images",
            breadcrumb: {
              i18nKey: "ota-images:breadcrumbs.images",
              fallback: "OTA Images",
            },
            subroutes: [
              {
                path: "/new",
                breadcrumb: {
                  i18nKey: "ota-images:breadcrumbs.new",
                  fallback: "Upload OTA Image",
                },
              },
            ],
          },
          {
            path: "/jobs",
            breadcrumb: {
              i18nKey: "ota-jobs:breadcrumbs.jobs",
              fallback: "OTA Jobs",
            },
            subroutes: [
              {
                path: "/new",
                breadcrumb: {
                  i18nKey: "ota-jobs:breadcrumbs.newJob",
                  fallback: "Create OTA Job",
                },
              },
              {
                path: "/$jobId",
                redirectTo: "/home/ota/jobs/$jobId/overview",
                subroutes: [
                  {
                    path: "/overview",
                    breadcrumb: {
                      i18nKey: "ota-jobs:breadcrumbs.overview",
                      fallback: "Overview",
                    },
                  },
                  {
                    path: "/nodes",
                    breadcrumb: {
                      i18nKey: "ota-jobs:breadcrumbs.nodes",
                      fallback: "Nodes",
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
      {
        path: "/settings",
        redirectTo: "/home/settings/voice-assistants",
        subroutes: [
          {
            path: "/voice-assistants",
            redirectTo: "/home/settings/voice-assistants/alexa",
            breadcrumb: {
              i18nKey: "common:sidebar.voiceAssistants",
              fallback: "Voice assistants",
            },
            subroutes: [
              {
                path: "/alexa",
                breadcrumb: {
                  i18nKey: "common:sidebar.alexa",
                  fallback: "Alexa",
                },
              },
              {
                path: "/gva",
                breadcrumb: {
                  i18nKey: "common:sidebar.gva",
                  fallback: "GVA",
                },
              },
              {
                path: "/smartthings",
                breadcrumb: {
                  i18nKey: "common:sidebar.smartthings",
                  fallback: "SmartThings",
                },
              },
            ],
          },
          {
            path: "/push-notifications",
            breadcrumb: {
              i18nKey: "common:sidebar.pushNotifications",
              fallback: "Push notifications",
            },
          },
          {
            path: "/post-deployment",
            breadcrumb: {
              i18nKey: "common:sidebar.postDeployment",
              fallback: "Post-Deployment",
            },
          },
        ],
      },
      {
        path: ACCOUNT_SETTINGS_ROUTE_SEGMENT,
        redirectTo: DEFAULT_ACCOUNT_SETTINGS_TAB.path,
        breadcrumb: {
          i18nKey: "account-settings:breadcrumbs.accountSettings",
          fallback: "Account settings",
        },
        subroutes: accountSettingsSubroutes,
      },
    ],
  },
];

/**
 * Top-level path prefixes whose subtree renders without runtime config.
 *
 * Derived from `routesConfig` rather than hand-listed so the exemption cannot drift from
 * the route tree. Prefix matching is why only top-level entries need the flag — declaring
 * it on a parent covers every descendant.
 */
export const BOOTSTRAP_EXEMPT_PATHS: readonly string[] = routesConfig
  .filter((route) => route.skipBootstrap)
  .map((route) => route.path);
