/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import type { JobStatus, TargetSelection } from "@aws-sdk/client-iot";
import { useAuthStore } from "@/stores/auth.store";
import {
  cancelOTAJob,
  createOTAJob,
  deleteOTAJob,
  describeJob,
  describeJobExecution,
  listJobExecutionsForJob,
  listJobExecutionsForThing,
  listOTAJobs,
  OTA_JOB_PREFIX,
  type CreateOtaJobParams,
} from "@/aws/services/ota.service";

export interface OtaJobsListParams {
  pageSize: number;
  nextToken?: string;
  status?: JobStatus;
  targetSelection?: TargetSelection;
  thingGroupName?: string;
}

export interface ThingOtaJobsListParams {
  thingName: string;
  pageSize: number;
  nextToken?: string;
}

export interface GroupOtaJobsListParams {
  groupName: string;
  pageSize: number;
  nextToken?: string;
}

export interface JobExecutionsListParams {
  jobId: string;
  pageSize: number;
  nextToken?: string;
}

export const otaJobsKeys = {
  all: ["iot", "ota-jobs"] as const,
  detail: (jobId: string) => [...otaJobsKeys.all, "detail", jobId] as const,
  list: (params: OtaJobsListParams) =>
    [...otaJobsKeys.all, "list", params] as const,
  forThing: (params: ThingOtaJobsListParams) =>
    [
      ...otaJobsKeys.all,
      "for-thing",
      params.thingName,
      { pageSize: params.pageSize, nextToken: params.nextToken },
    ] as const,
  forGroup: (params: GroupOtaJobsListParams) =>
    [
      ...otaJobsKeys.all,
      "for-group",
      params.groupName,
      { pageSize: params.pageSize, nextToken: params.nextToken },
    ] as const,
  executionsForJob: (params: JobExecutionsListParams) =>
    [
      ...otaJobsKeys.all,
      "executions",
      params.jobId,
      { pageSize: params.pageSize, nextToken: params.nextToken },
    ] as const,
  executionDetail: (jobId: string, thingName: string) =>
    [...otaJobsKeys.all, "execution", jobId, thingName] as const,
};

export const otaJobsQueries = {
  detail: (jobId: string) =>
    queryOptions({
      queryKey: otaJobsKeys.detail(jobId),
      // Normalize a missing job to `null` so consumers can distinguish
      // "loaded but not found" and React Query never sees `undefined`.
      queryFn: async () => (await describeJob(jobId)) ?? null,
    }),
  list: (params: OtaJobsListParams) =>
    queryOptions({
      queryKey: otaJobsKeys.list(params),
      queryFn: () =>
        listOTAJobs({
          maxResults: params.pageSize,
          nextToken: params.nextToken,
          status: params.status,
          targetSelection: params.targetSelection,
          thingGroupName: params.thingGroupName,
        }),
      placeholderData: keepPreviousData,
    }),
  forThing: (params: ThingOtaJobsListParams) =>
    queryOptions({
      queryKey: otaJobsKeys.forThing(params),
      queryFn: () =>
        listJobExecutionsForThing({
          thingName: params.thingName,
          maxResults: params.pageSize,
          nextToken: params.nextToken,
        }),
      placeholderData: keepPreviousData,
    }),
  forGroup: (params: GroupOtaJobsListParams) =>
    queryOptions({
      queryKey: otaJobsKeys.forGroup(params),
      queryFn: () =>
        listOTAJobs({
          thingGroupName: params.groupName,
          maxResults: params.pageSize,
          nextToken: params.nextToken,
        }),
      placeholderData: keepPreviousData,
    }),
  executionsForJob: (params: JobExecutionsListParams) =>
    queryOptions({
      queryKey: otaJobsKeys.executionsForJob(params),
      queryFn: () =>
        listJobExecutionsForJob({
          jobId: params.jobId,
          maxResults: params.pageSize,
          nextToken: params.nextToken,
        }),
      placeholderData: keepPreviousData,
    }),
  executionDetail: (jobId: string, thingName: string) =>
    queryOptions({
      queryKey: otaJobsKeys.executionDetail(jobId, thingName),
      // Normalize a missing execution to `null` so consumers can distinguish
      // "loaded but not found" and React Query never sees `undefined`.
      queryFn: async () =>
        (await describeJobExecution({ jobId, thingName })) ?? null,
    }),
};

export function useOtaJobDetailsQuery(jobId?: string) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...otaJobsQueries.detail(jobId ?? ""),
    enabled: !!credentials && !!jobId,
  });
}

export function useOtaJobsListQuery(params: OtaJobsListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...otaJobsQueries.list(params),
    enabled: !!credentials,
  });
}

export function useThingOtaJobExecutionsQuery(params: ThingOtaJobsListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...otaJobsQueries.forThing(params),
    enabled: !!credentials && !!params.thingName,
  });
}

export function useGroupOtaJobsListQuery(params: GroupOtaJobsListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...otaJobsQueries.forGroup(params),
    enabled: !!credentials && !!params.groupName,
  });
}

export function useOtaJobExecutionsQuery(params: JobExecutionsListParams) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...otaJobsQueries.executionsForJob(params),
    enabled: !!credentials && !!params.jobId,
  });
}

export function useOtaJobExecutionDetailQuery(
  jobId?: string,
  thingName?: string,
) {
  const credentials = useAuthStore((s) => s.credentials);
  return useQuery({
    ...otaJobsQueries.executionDetail(jobId ?? "", thingName ?? ""),
    enabled: !!credentials && !!jobId && !!thingName,
  });
}

export function useCreateOtaJobMutation() {
  const queryClient = useQueryClient();
  return useMutation<{ jobId: string }, Error, CreateOtaJobParams>({
    mutationFn: async (params) => {
      const response = await createOTAJob(params);
      // Prefer the AWS-assigned job id; fall back to the deterministic id the
      // service derives from the name so navigation always has a target.
      return { jobId: response.jobId ?? `${OTA_JOB_PREFIX}${params.otaUpdateId}` };
    },
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: otaJobsKeys.all });
    },
  });
}

export function useCancelOtaJobMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (jobId: string) => cancelOTAJob(jobId),
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: otaJobsKeys.all });
    },
  });
}

export function useDeleteOtaJobMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (jobId: string) => deleteOTAJob(jobId),
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: otaJobsKeys.all });
    },
  });
}
