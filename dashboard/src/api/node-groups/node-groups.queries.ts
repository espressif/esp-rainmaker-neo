/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useMemo } from "react";
import {
  keepPreviousData,
  queryOptions,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { useAuthStore } from "@/stores/auth.store";
import { isTransientNodeGroupStatus } from "@/config/node-group-status.config";
import { isDynamicNodeGroup } from "@/config/node-group-type.config";
import {
  addThingToThingGroup,
  createDynamicThingGroup,
  createThingGroup,
  deleteDynamicThingGroup,
  deleteThingGroup,
  describeThingGroup,
  listThingsInThingGroup,
  removeThingFromThingGroup,
  searchThingGroups,
  type CreateNodeGroupRequest,
} from "@/aws/services/thing-group.service";
import { searchThings } from "@/aws/services/thing.service";
import {
  extractIparamsFields,
  type IparamsFields,
} from "@/pages/home/node-management/nodes/iparams-fields";

export type NodeGroupsSearchField = "groupName" | "description";

export interface NodeGroupsListParams {
  pageSize: number;
  nextToken?: string;
  searchField?: NodeGroupsSearchField;
  searchValue?: string;
}

function buildQueryString(params: NodeGroupsListParams): string {
  const term = params.searchValue?.trim();
  if (!term) {
    return "*";
  }
  if (params.searchField === "description") {
    return `description:*${term}*`;
  }
  return `thingGroupName:*${term}*`;
}

export interface GroupNodesListParams {
  groupName: string;
  pageSize: number;
  nextToken?: string;
}

const TRANSIENT_STATUS_POLL_MS = 5000;

export const nodeGroupsKeys = {
  all: ["iot", "node-groups"] as const,
  /** Prefix for every list query, so a delete can refresh lists without touching details. */
  lists: ["iot", "node-groups", "list"] as const,
  list: (params: NodeGroupsListParams) =>
    [...nodeGroupsKeys.all, "list", params] as const,
  detail: (groupName: string) =>
    [...nodeGroupsKeys.all, "detail", groupName] as const,
  groupNodes: (params: GroupNodesListParams) =>
    [...nodeGroupsKeys.all, "group-nodes", params] as const,
  groupNodesEnrichment: (groupName: string, thingNames: string[]) =>
    [
      ...nodeGroupsKeys.all,
      "group-nodes-enrichment",
      groupName,
      [...thingNames].sort(),
    ] as const,
  groupThingNames: (groupName: string) =>
    [...nodeGroupsKeys.all, "group-thing-names", groupName] as const,
};

export const nodeGroupsQueries = {
  list: (params: NodeGroupsListParams) =>
    queryOptions({
      queryKey: nodeGroupsKeys.list(params),
      queryFn: () =>
        searchThingGroups({
          queryString: buildQueryString(params),
          maxResults: params.pageSize,
          nextToken: params.nextToken,
        }),
      placeholderData: keepPreviousData,
    }),
  detail: (groupName: string) =>
    queryOptions({
      queryKey: nodeGroupsKeys.detail(groupName),
      queryFn: () => describeThingGroup({ thingGroupName: groupName }),
      // A dynamic group resolves BUILDING/REBUILDING within seconds; poll until
      // it settles so the status badge does not sit spinning until a reload.
      refetchInterval: (query) =>
        isTransientNodeGroupStatus(query.state.data?.status)
          ? TRANSIENT_STATUS_POLL_MS
          : false,
    }),
};

export function useNodeGroupsListQuery(params: NodeGroupsListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...nodeGroupsQueries.list(params),
    enabled: !!credentials,
  });
}

export function useNodeGroupDetailsQuery(groupName: string | undefined) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...nodeGroupsQueries.detail(groupName ?? ""),
    enabled: !!credentials && !!groupName,
  });
}

export function useCreateNodeGroupMutation() {
  const queryClient = useQueryClient();
  return useMutation<{ groupName: string }, Error, CreateNodeGroupRequest>({
    mutationFn: async (request) => {
      if (request.kind === "dynamic") {
        await createDynamicThingGroup({
          thingGroupName: request.thingGroupName,
          queryString: request.queryString,
          description: request.description,
        });
      } else {
        await createThingGroup({
          thingGroupName: request.thingGroupName,
          ...(request.parentGroupName && {
            parentGroupName: request.parentGroupName,
          }),
          ...(request.description && {
            thingGroupProperties: {
              thingGroupDescription: request.description,
            },
          }),
        });
      }
      return { groupName: request.thingGroupName };
    },
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: nodeGroupsKeys.all });
    },
  });
}

/**
 * Static and dynamic groups are distinct AWS REST resources: DeleteThingGroup only addresses
 * `/thing-groups/{name}`, while a dynamic group lives at `/dynamic-thing-groups/{name}`. Callers
 * (notably the list page, whose payload carries no `queryString`) cannot know which they have, so
 * the type is resolved here. On the details page this is a cache hit; from the list it costs one
 * DescribeThingGroup.
 */
export function useDeleteNodeGroupMutation() {
  const queryClient = useQueryClient();
  return useMutation<void, Error, string>({
    mutationFn: async (groupName) => {
      const group = await queryClient.ensureQueryData(
        nodeGroupsQueries.detail(groupName),
      );
      if (isDynamicNodeGroup(group.queryString)) {
        await deleteDynamicThingGroup(groupName);
        return;
      }
      await deleteThingGroup({ thingGroupName: groupName });
    },
    onSuccess: (_data, groupName) => {
      // Drop the detail outright rather than invalidating it — refetching a deleted group 404s.
      queryClient.removeQueries({
        queryKey: nodeGroupsKeys.detail(groupName),
      });
      return queryClient.invalidateQueries({ queryKey: nodeGroupsKeys.lists });
    },
  });
}

/**
 * Derives static/dynamic for a group. Tab pages cannot receive props through `<Outlet/>`, so they
 * read this instead; it shares the parent route's cached detail query, so it costs no extra request.
 */
export function useNodeGroupTypeQuery(groupName: string | undefined) {
  const query = useNodeGroupDetailsQuery(groupName);
  const queryString = query.data?.queryString ?? null;
  return {
    queryString,
    isDynamic: isDynamicNodeGroup(queryString),
    isPending: query.isPending,
    isError: query.isError,
  };
}

export function useGroupNodesListQuery(params: GroupNodesListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    queryKey: nodeGroupsKeys.groupNodes(params),
    queryFn: () =>
      listThingsInThingGroup({
        thingGroupName: params.groupName,
        recursive: true,
        maxResults: params.pageSize,
        nextToken: params.nextToken,
      }),
    enabled: !!credentials && !!params.groupName,
    placeholderData: keepPreviousData,
  });
}

export type GroupNodesEnrichmentMap = Record<string, IparamsFields>;

export function useGroupNodesEnrichmentQuery(
  groupName: string,
  thingNames: string[],
) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery<GroupNodesEnrichmentMap>({
    queryKey: nodeGroupsKeys.groupNodesEnrichment(groupName, thingNames),
    queryFn: async () => {
      const queryString = thingNames
        .map((name) => `thingName:"${name}"`)
        .join(" OR ");
      const response = await searchThings({
        indexName: "AWS_Things",
        queryString: `(${queryString})`,
        maxResults: thingNames.length,
      });
      const map: GroupNodesEnrichmentMap = {};
      for (const thing of response.things) {
        const key = thing.thingName;
        if (!key) {
          continue;
        }
        const fields = extractIparamsFields(thing.shadow);
        const timestamp = thing.connectivity?.timestamp;
        if (timestamp != null && fields.lastSeen == null) {
          fields.lastSeen = Math.floor(timestamp / 1000);
        }
        map[key] = fields;
      }
      return map;
    },
    enabled: !!credentials && thingNames.length > 0,
    retry: false,
    staleTime: 30_000,
  });
}

type GroupThingsPage = { things: string[]; nextToken?: string };
type GroupThingsData = InfiniteData<GroupThingsPage>;

// AWS IoT ListThingsInThingGroup is eventually consistent; give it a moment
// before reconciling optimistic cache with the server.
const RECONCILE_DELAY_MS = 4000;

function scheduleReconcile(
  queryClient: ReturnType<typeof useQueryClient>,
): void {
  setTimeout(() => {
    void queryClient.invalidateQueries({ queryKey: nodeGroupsKeys.all });
  }, RECONCILE_DELAY_MS);
}

export function useRemoveThingFromGroupMutation() {
  const queryClient = useQueryClient();
  return useMutation<
    void,
    Error,
    { groupName: string; thingName: string },
    { previous: GroupThingsData | undefined }
  >({
    mutationFn: async ({ groupName, thingName }) => {
      await removeThingFromThingGroup({
        thingGroupName: groupName,
        thingName,
      });
    },
    onMutate: async ({ groupName, thingName }) => {
      const key = nodeGroupsKeys.groupThingNames(groupName);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<GroupThingsData>(key);
      queryClient.setQueryData<GroupThingsData>(key, (old) => {
        if (!old) {
          return old;
        }
        return {
          ...old,
          pages: old.pages.map((page) => ({
            ...page,
            things: page.things.filter((name) => name !== thingName),
          })),
        };
      });
      return { previous };
    },
    onError: (_err, { groupName }, context) => {
      if (context?.previous !== undefined) {
        queryClient.setQueryData(
          nodeGroupsKeys.groupThingNames(groupName),
          context.previous,
        );
      }
    },
    onSettled: () => {
      scheduleReconcile(queryClient);
    },
  });
}

export function useAddThingToGroupMutation() {
  const queryClient = useQueryClient();
  return useMutation<
    void,
    Error,
    { groupName: string; thingName: string },
    { previous: GroupThingsData | undefined }
  >({
    mutationFn: async ({ groupName, thingName }) => {
      await addThingToThingGroup({
        thingGroupName: groupName,
        thingName,
      });
    },
    onMutate: async ({ groupName, thingName }) => {
      const key = nodeGroupsKeys.groupThingNames(groupName);
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<GroupThingsData>(key);
      queryClient.setQueryData<GroupThingsData>(key, (old) => {
        if (!old || old.pages.length === 0) {
          return {
            pages: [{ things: [thingName] }],
            pageParams: [undefined],
          };
        }
        const alreadyPresent = old.pages.some((page) =>
          page.things.includes(thingName),
        );
        if (alreadyPresent) {
          return old;
        }
        const [first, ...rest] = old.pages;
        return {
          ...old,
          pages: [
            { ...first, things: [thingName, ...first.things] },
            ...rest,
          ],
        };
      });
      return { previous };
    },
    onError: (_err, { groupName }, context) => {
      if (context?.previous !== undefined) {
        queryClient.setQueryData(
          nodeGroupsKeys.groupThingNames(groupName),
          context.previous,
        );
      }
    },
    onSettled: () => {
      scheduleReconcile(queryClient);
    },
  });
}

const GROUP_THING_NAMES_PAGE_SIZE = 100;

export function useGroupThingNamesSetQuery(groupName: string) {
  const credentials = useAuthStore((s) => s.credentials);
  const query = useInfiniteQuery({
    queryKey: nodeGroupsKeys.groupThingNames(groupName),
    queryFn: ({ pageParam }) =>
      listThingsInThingGroup({
        thingGroupName: groupName,
        maxResults: GROUP_THING_NAMES_PAGE_SIZE,
        nextToken: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextToken ?? undefined,
    enabled: !!credentials && !!groupName,
  });

  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query;

  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage) {
      void fetchNextPage();
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const set = useMemo(
    () => new Set((query.data?.pages ?? []).flatMap((p) => p.things)),
    [query.data],
  );

  return {
    set,
    isLoading: query.isLoading,
    isFetchingAll: query.isFetchingNextPage,
  };
}
