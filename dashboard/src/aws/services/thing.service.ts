/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  DescribeThingCommand,
  GetBucketsAggregationCommand,
  ListThingsCommand,
  SearchIndexCommand,
  type DescribeThingCommandInput,
  type ListThingsCommandInput,
  type SearchIndexCommandInput,
} from "@aws-sdk/client-iot";
import { getIoTClient } from "./client";

export async function listThings(params: ListThingsCommandInput) {
  const client = getIoTClient();
  const response = await client.send(new ListThingsCommand(params));
  return {
    things: response.things ?? [],
    nextToken: response.nextToken,
  };
}

export async function describeThing(params: DescribeThingCommandInput) {
  const client = getIoTClient();
  return await client.send(new DescribeThingCommand(params));
}

export async function getFieldValues(
  aggregationField: string,
  queryString: string = "*",
  indexName: string = "AWS_Things",
): Promise<{ value: string; count: number }[]> {
  const client = getIoTClient();
  const response = await client.send(
    new GetBucketsAggregationCommand({
      queryString,
      indexName,
      aggregationField,
      bucketsAggregationType: {
        termsAggregation: { maxBuckets: 50 },
      },
    }),
  );
  return (response.buckets ?? [])
    .filter((b): b is typeof b & { keyValue: string } => b.keyValue != null)
    .map((b) => ({ value: b.keyValue, count: b.count ?? 0 }));
}

export async function searchThings(params: SearchIndexCommandInput) {
  const client = getIoTClient();
  const response = await client.send(new SearchIndexCommand(params));
  return {
    things: (response.things ?? []).map((thing) => ({
      ...thing,
      shadow: thing.shadow ?? undefined,
      connectivity: thing.connectivity ?? undefined,
      thingGroupNames: thing.thingGroupNames ?? [],
    })),
    nextToken: response.nextToken,
  };
}
