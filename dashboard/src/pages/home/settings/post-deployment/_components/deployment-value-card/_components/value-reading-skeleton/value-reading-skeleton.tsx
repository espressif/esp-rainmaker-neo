/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { Skeleton } from "@espressif/dashboard-ui-components/components";

/** Mirrors the two rows of {@link ../value-detail-list} so the card does not jump on load. */
export default function ValueReadingSkeleton() {
  return (
    <div className="space-y-4" aria-busy>
      <div className="flex items-center justify-between gap-4">
        <Skeleton className="h-4 w-20" />
        <Skeleton className="h-6 w-40" />
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-3 w-full" />
        <Skeleton className="h-3 w-4/5" />
      </div>
    </div>
  );
}
