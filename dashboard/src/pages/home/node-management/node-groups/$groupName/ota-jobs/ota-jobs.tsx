/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { GroupOtaJobsMainContent } from "./_components/group-ota-jobs-main-content";
import { useRouteParams } from "@/lib/navigation/use-route-params";

export default function GroupOtaJobsPage() {
  const params = useRouteParams<{ groupName?: string }>();
  const groupName = params.groupName;
  if (!groupName) {
    return null;
  }
  return <GroupOtaJobsMainContent groupName={groupName} />;
}
