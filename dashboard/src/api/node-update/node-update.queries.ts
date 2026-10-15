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
} from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth.store'
import { downloadCertCsv } from '@/lib/registration-jobs/download-cert-csv'
import { nodeUpdateApi } from './node-update.api'
import type {
  CreateBulkUpdateJobRequest,
  ListUpdateJobsResponse,
} from './node-update.types'

export type UpdateStepId =
  | 'initiate-registration'
  | 'upload-file'
  | 'create-request'

export type UpdateStepState = 'in_progress' | 'success' | 'error'

export interface UpdateStepMessages {
  inProgress: string
  success: string
  errorFallback: string
}

export type UpdateProgressCallback = (
  stepId: UpdateStepId,
  state: UpdateStepState,
  description?: string,
) => void

export interface UpdateNodesParams {
  file: File
  adminGroupNames?: string[]
  adminParentGroupName?: string
  tags?: string[]
  onProgress?: UpdateProgressCallback
  stepMessages?: Record<UpdateStepId, UpdateStepMessages>
}

export interface UpdateNodesResult {
  requestId: string
}

export interface UpdateJobsListParams {
  pageSize: number
  startKey?: string
  status?: string
}

const IN_PROGRESS_STATUSES = ['requested', 'started', 'data_loaded']

export const updateJobsKeys = {
  all: ['update-jobs'] as const,
  list: (params: UpdateJobsListParams) =>
    [...updateJobsKeys.all, 'list', params] as const,
  detail: (requestId: string) =>
    [...updateJobsKeys.all, 'detail', requestId] as const,
}

export const updateJobsQueries = {
  list: (params: UpdateJobsListParams) =>
    queryOptions<ListUpdateJobsResponse>({
      queryKey: updateJobsKeys.list(params),
      queryFn: () =>
        nodeUpdateApi.listUpdateJobs(
          params.pageSize,
          params.startKey,
          params.status,
        ),
      placeholderData: keepPreviousData,
      refetchInterval: (query) => {
        const jobs = query.state.data?.jobs
        if (!jobs) {
          return false
        }
        const hasInProgress = jobs.some((j) =>
          IN_PROGRESS_STATUSES.includes(j.status),
        )
        return hasInProgress ? 10000 : false
      },
    }),
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) {return error.message}
  return fallback
}

async function runStep<T>(
  stepId: UpdateStepId,
  work: () => Promise<T>,
  onProgress: UpdateProgressCallback | undefined,
  messages: UpdateStepMessages | undefined,
): Promise<T> {
  onProgress?.(stepId, 'in_progress', messages?.inProgress)
  try {
    const result = await work()
    onProgress?.(stepId, 'success', messages?.success)
    return result
  } catch (error) {
    onProgress?.(
      stepId,
      'error',
      errorMessage(error, messages?.errorFallback ?? 'Step failed'),
    )
    throw error
  }
}

/**
 * Non-blocking mutation: uploads CSV, creates the job, returns request_id immediately.
 * Does NOT poll for completion.
 */
export function useUpdateNodes() {
  const queryClient = useQueryClient()
  return useMutation<UpdateNodesResult, Error, UpdateNodesParams>({
    mutationFn: async (params) => {
      const { onProgress, stepMessages } = params

      const { upload_url, s3_path } = await runStep(
        'initiate-registration',
        () => nodeUpdateApi.getFileUploadUrl(params.file.name),
        onProgress,
        stepMessages?.['initiate-registration'],
      )

      await runStep(
        'upload-file',
        () => nodeUpdateApi.uploadFileToS3(upload_url, params.file),
        onProgress,
        stepMessages?.['upload-file'],
      )

      const jobRequest: CreateBulkUpdateJobRequest = {
        cert_file_s3_path: s3_path,
      }
      if (params.adminGroupNames && params.adminGroupNames.length > 0) {
        jobRequest.admin_group_names = params.adminGroupNames
      }
      if (params.adminParentGroupName) {
        jobRequest.admin_parent_group_name = params.adminParentGroupName
      }
      if (params.tags && params.tags.length > 0) {
        jobRequest.tags = params.tags
      }

      const { request_id } = await runStep(
        'create-request',
        () => nodeUpdateApi.createBulkUpdateJob(jobRequest),
        onProgress,
        stepMessages?.['create-request'],
      )
      return { requestId: request_id }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: updateJobsKeys.all })
    },
  })
}

/**
 * Poll a single registration job's status.
 * Automatically refetches every 3s while the job is in progress.
 */
export function useUpdateJobStatus(requestId: string | null) {
  return useQuery({
    queryKey: updateJobsKeys.detail(requestId ?? ''),
    queryFn: () => {
      if (!requestId) {
        throw new Error('requestId is required')
      }
      return nodeUpdateApi.getUpdateJobStatus(requestId)
    },
    enabled: !!requestId,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      if (!status) {return 3000}
      if (status === 'success' || status === 'completed' || status === 'error' || status === 'failed') {
        return false // Stop polling
      }
      return 3000
    },
  })
}

/**
 * Download a registration job's cert CSV (uploaded certs or the failed-rows file).
 * The S3 object is presigned at call time, so callers pass the `s3://` path rather
 * than a URL that could have expired.
 */
export function useDownloadUpdateCsv() {
  return useMutation<void, Error, string>({
    mutationFn: (s3Path) => downloadCertCsv(s3Path),
  })
}

/**
 * List registration jobs. Auto-refetches every 10s while any job in the current
 * page is still in progress.
 */
export function useUpdateJobs(params: UpdateJobsListParams) {
  const credentials = useAuthStore((s) => s.credentials)
  return useQuery({
    ...updateJobsQueries.list(params),
    enabled: !!credentials,
  })
}
