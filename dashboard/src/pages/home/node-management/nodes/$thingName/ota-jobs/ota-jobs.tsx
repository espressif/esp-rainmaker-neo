/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ThingOtaJobsMainContent } from "./_components/thing-ota-jobs-main-content";
import { useRouteParams } from "@/lib/navigation/use-route-params";

export default function ThingOtaJobsPage() {
  const params = useRouteParams<{ thingName?: string }>();
  const thingName = params.thingName;

  if (!thingName) {
    return null;
  }

  return <ThingOtaJobsMainContent thingName={thingName} />;
}
