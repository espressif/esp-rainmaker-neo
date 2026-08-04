/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  AddThingToThingGroupCommand,
  CreateThingGroupCommand,
  CreateDynamicThingGroupCommand,
  DeleteThingGroupCommand,
  DeleteDynamicThingGroupCommand,
  DescribeThingGroupCommand,
  ListThingGroupsCommand,
  ListThingsInThingGroupCommand,
  RemoveThingFromThingGroupCommand,
  SearchIndexCommand,
  UpdateThingGroupCommand,
  type AddThingToThingGroupCommandInput,
  type CreateThingGroupCommandInput,
  type DeleteThingGroupCommandInput,
  type DescribeThingGroupCommandInput,
  type ListThingGroupsCommandInput,
  type ListThingsInThingGroupCommandInput,
  type RemoveThingFromThingGroupCommandInput,
  type UpdateThingGroupCommandInput,
} from "@aws-sdk/client-iot";
import { getIoTClient } from "./client";

export async function listThingGroups(params: ListThingGroupsCommandInput) {
  const client = getIoTClient();
  const response = await client.send(new ListThingGroupsCommand(params));
  return {
    thingGroups: response.thingGroups ?? [],
    nextToken: response.nextToken,
  };
}

export async function describeThingGroup(
  params: DescribeThingGroupCommandInput,
) {
  const client = getIoTClient();
  return await client.send(new DescribeThingGroupCommand(params));
}

/**
 * Request for creating a node group. The unified create form maps to two
 * distinct AWS IoT calls, so the shape is discriminated on `kind`:
 * - `static`  → CreateThingGroup (plain group, or nested when `parentGroupName` is set)
 * - `dynamic` → CreateDynamicThingGroup (populated from a fleet-index `queryString`)
 */
export type CreateNodeGroupRequest =
  | {
      kind: "static";
      thingGroupName: string;
      description?: string;
      parentGroupName?: string;
    }
  | {
      kind: "dynamic";
      thingGroupName: string;
      description?: string;
      queryString: string;
    };

export async function createThingGroup(params: CreateThingGroupCommandInput) {
  const client = getIoTClient();
  return await client.send(new CreateThingGroupCommand(params));
}

export async function createDynamicThingGroup(params: {
  thingGroupName: string;
  queryString: string;
  description?: string;
}) {
  const client = getIoTClient();
  return await client.send(
    new CreateDynamicThingGroupCommand({
      thingGroupName: params.thingGroupName,
      queryString: params.queryString,
      indexName: "AWS_Things",
      ...(params.description && {
        thingGroupProperties: {
          thingGroupDescription: params.description,
        },
      }),
    })
  );
}

export async function deleteDynamicThingGroup(thingGroupName: string) {
  const client = getIoTClient();
  await client.send(new DeleteDynamicThingGroupCommand({ thingGroupName }));
}

export async function deleteThingGroup(params: DeleteThingGroupCommandInput) {
  const client = getIoTClient();
  await client.send(new DeleteThingGroupCommand(params));
}

export async function listThingsInThingGroup(
  params: ListThingsInThingGroupCommandInput,
) {
  const client = getIoTClient();
  const response = await client.send(
    new ListThingsInThingGroupCommand(params),
  );
  return {
    things: response.things ?? [],
    nextToken: response.nextToken,
  };
}

export async function addThingToThingGroup(
  params: AddThingToThingGroupCommandInput,
) {
  const client = getIoTClient();
  await client.send(new AddThingToThingGroupCommand(params));
}

export async function updateThingGroup(params: UpdateThingGroupCommandInput) {
  const client = getIoTClient();
  return await client.send(new UpdateThingGroupCommand(params));
}

export async function removeThingFromThingGroup(
  params: RemoveThingFromThingGroupCommandInput,
) {
  const client = getIoTClient();
  await client.send(new RemoveThingFromThingGroupCommand(params));
}

export async function searchThingGroups(params: {
  queryString: string;
  maxResults?: number;
  nextToken?: string;
}) {
  const client = getIoTClient();
  const response = await client.send(
    new SearchIndexCommand({
      queryString: params.queryString,
      indexName: "AWS_ThingGroups",
      maxResults: params.maxResults,
      nextToken: params.nextToken,
    }),
  );
  return {
    thingGroups: (response.thingGroups ?? []).map((g) => ({
      groupName: g.thingGroupName ?? "",
      groupId: g.thingGroupId ?? "",
      groupDescription: g.thingGroupDescription ?? "",
      parentGroupNames: g.parentGroupNames ?? [],
    })),
    nextToken: response.nextToken,
  };
}
