/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  ListStacksCommand,
  type StackStatus,
} from "@aws-sdk/client-cloudformation";
import { getCloudFormationClient } from "./client";

// "Live" stack statuses — excludes deleted/failed so a torn-down or never-finished module doesn't count.
const ACTIVE_STATUSES: StackStatus[] = [
  "CREATE_COMPLETE",
  "UPDATE_COMPLETE",
  "UPDATE_ROLLBACK_COMPLETE",
  "IMPORT_COMPLETE",
  "IMPORT_ROLLBACK_COMPLETE",
];

/** Deployed CloudFormation stack names in this account/region — used to show optional-module features only when their stack is present. */
export async function listDeployedStackNames(): Promise<Set<string>> {
  const client = getCloudFormationClient();
  const names = new Set<string>();
  let nextToken: string | undefined;

  do {
    const res = await client.send(
      new ListStacksCommand({
        StackStatusFilter: ACTIVE_STATUSES,
        NextToken: nextToken,
      }),
    );
    for (const s of res.StackSummaries ?? []) {
      if (s.StackName) {
        names.add(s.StackName);
      }
    }
    nextToken = res.NextToken;
  } while (nextToken);

  return names;
}
