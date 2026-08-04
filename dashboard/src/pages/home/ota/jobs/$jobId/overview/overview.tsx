/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useOtaJobDetailsQuery } from "@/api/ota-jobs";
import OtaJobOverviewContent from "./_components/ota-job-overview-content/ota-job-overview-content";
import { useRouteParams } from "@/lib/navigation/use-route-params";

export default function OtaJobOverviewPage() {
  const params = useRouteParams<{ jobId?: string }>();
  // The shell (`job-details.tsx`) gates loading/error/not-found before mounting
  // this tab, and React Query dedupes on `otaJobsKeys.detail(jobId)` — so this
  // is a cache read, not a second request.
  const { data } = useOtaJobDetailsQuery(params.jobId);

  if (!params.jobId || !data) {
    return null;
  }

  return <OtaJobOverviewContent job={data} />;
}
