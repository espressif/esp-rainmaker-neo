/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import type { TFunction } from "i18next";
import type { RouteConfig } from "@/config/app-routes.config";
import { buildBreadcrumbs } from "./build-breadcrumbs";

const labels: Record<string, string> = {
  "common:breadcrumbs.home": "Home",
  "common:sidebar.nodes": "Nodes",
  "common:sidebar.nodeGroups": "Node groups",
  "common:sidebar.otaJobs": "Jobs",
  "common:sidebar.voiceAssistants": "Voice assistants",
  "common:sidebar.alexa": "Alexa",
  "common:sidebar.pushNotifications": "Push notifications",
};

const t = ((key: string, fallback?: string) =>
  labels[key] ?? fallback ?? key) as TFunction;

const testRoutes: RouteConfig[] = [
  {
    path: "/home",
    breadcrumb: { i18nKey: "common:breadcrumbs.home", fallback: "Home" },
    subroutes: [
      {
        path: "/node-management",
        subroutes: [
          {
            path: "/nodes",
            breadcrumb: {
              i18nKey: "common:sidebar.nodes",
              fallback: "Nodes",
            },
            subroutes: [{ path: "/$thingName" }],
          },
          {
            path: "/node-groups",
            breadcrumb: {
              i18nKey: "common:sidebar.nodeGroups",
              fallback: "Node groups",
            },
            subroutes: [{ path: "/$groupName" }],
          },
        ],
      },
      {
        path: "/ota",
        subroutes: [
          {
            path: "/jobs",
            breadcrumb: {
              i18nKey: "common:sidebar.otaJobs",
              fallback: "Jobs",
            },
            subroutes: [{ path: "/$jobId" }],
          },
        ],
      },
      {
        path: "/settings",
        subroutes: [
          {
            path: "/voice-assistants",
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
            ],
          },
          {
            path: "/push-notifications",
            breadcrumb: {
              i18nKey: "common:sidebar.pushNotifications",
              fallback: "Push notifications",
            },
          },
        ],
      },
    ],
  },
];

describe("buildBreadcrumbs", () => {
  it("builds crumbs for a static list route with a link on the parent", () => {
    expect(buildBreadcrumbs(testRoutes, "/home/node-management/nodes", t)).toEqual([
      { label: "Home", path: "/home" },
      { label: "Nodes" },
    ]);
  });

  it("skips dynamic segments and keeps the list page as the current crumb", () => {
    expect(
      buildBreadcrumbs(testRoutes, "/home/node-management/nodes/device-1", t),
    ).toEqual([
      { label: "Home", path: "/home" },
      { label: "Nodes" },
    ]);
  });

  it("builds crumbs for OTA job detail paths", () => {
    expect(buildBreadcrumbs(testRoutes, "/home/ota/jobs/job-123", t)).toEqual([
      { label: "Home", path: "/home" },
      { label: "Jobs" },
    ]);
  });

  it("builds crumbs for voice assistant routes", () => {
    expect(
      buildBreadcrumbs(testRoutes, "/home/settings/voice-assistants/alexa", t),
    ).toEqual([
      { label: "Home", path: "/home" },
      { label: "Voice assistants", path: "/home/settings/voice-assistants" },
      { label: "Alexa" },
    ]);
  });

  it("builds crumbs for settings feature routes", () => {
    expect(
      buildBreadcrumbs(testRoutes, "/home/settings/push-notifications", t),
    ).toEqual([
      { label: "Home", path: "/home" },
      { label: "Push notifications" },
    ]);
  });

  it("returns only matching prefix crumbs for unknown paths", () => {
    expect(buildBreadcrumbs(testRoutes, "/home/nonexistent", t)).toEqual([
      { label: "Home" },
    ]);
  });

  it("builds crumbs for node group detail paths", () => {
    expect(
      buildBreadcrumbs(testRoutes, "/home/node-management/node-groups/my-group", t),
    ).toEqual([
      { label: "Home", path: "/home" },
      { label: "Node groups" },
    ]);
  });
});
