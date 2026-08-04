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
import { nodeRegistrationApi } from './node-registration.api'
import type {
  CreateBulkRegistrationJobRequest,
  ListRegistrationJobsResponse,
} from './node-registration.types'

export type RegistrationStepId =
  | 'initiate-registration'
  | 'upload-file'
  | 'create-request'

export type RegistrationStepState = 'in_progress' | 'success' | 'error'

export interface RegistrationStepMessages {
  inProgress: string
  success: string
  errorFallback: string
}

export type RegistrationProgressCallback = (
  stepId: RegistrationStepId,
  state: RegistrationStepState,
  description?: string,
) => void

export interface RegisterNodesParams {
  file: File
  adminGroupNames?: string[]
  adminParentGroupName?: string
  tags?: string[]
  capabilities?: string[]
  onProgress?: RegistrationProgressCallback
  stepMessages?: Record<RegistrationStepId, RegistrationStepMessages>
}

export interface RegisterNodesResult {
  requestId: string
}

export interface RegistrationJobsListParams {
  pageSize: number
  startKey?: string
  status?: string
}

const IN_PROGRESS_STATUSES = ['requested', 'started', 'data_loaded']

export const registrationJobsKeys = {
  all: ['registration-jobs'] as const,
  list: (params: RegistrationJobsListParams) =>
    [...registrationJobsKeys.all, 'list', params] as const,
  detail: (requestId: string) =>
    [...registrationJobsKeys.all, 'detail', requestId] as const,
}

export const registrationJobsQueries = {
  list: (params: RegistrationJobsListParams) =>
    queryOptions<ListRegistrationJobsResponse>({
      queryKey: registrationJobsKeys.list(params),
      queryFn: () =>
        nodeRegistrationApi.listRegistrationJobs(
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
  stepId: RegistrationStepId,
  work: () => Promise<T>,
  onProgress: RegistrationProgressCallback | undefined,
  messages: RegistrationStepMessages | undefined,
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
export function useRegisterNodes() {
  const queryClient = useQueryClient()
  return useMutation<RegisterNodesResult, Error, RegisterNodesParams>({
    mutationFn: async (params) => {
      const { onProgress, stepMessages } = params

      const { upload_url, s3_path } = await runStep(
        'initiate-registration',
        () => nodeRegistrationApi.getFileUploadUrl(params.file.name),
        onProgress,
        stepMessages?.['initiate-registration'],
      )

      await runStep(
        'upload-file',
        () => nodeRegistrationApi.uploadFileToS3(upload_url, params.file),
        onProgress,
        stepMessages?.['upload-file'],
      )

      const jobRequest: CreateBulkRegistrationJobRequest = {
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
      if (params.capabilities && params.capabilities.length > 0) {
        jobRequest.capabilities = params.capabilities
      }

      const { request_id } = await runStep(
        'create-request',
        () => nodeRegistrationApi.createBulkRegistrationJob(jobRequest),
        onProgress,
        stepMessages?.['create-request'],
      )
      return { requestId: request_id }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: registrationJobsKeys.all })
    },
  })
}

/**
 * Poll a single registration job's status.
 * Automatically refetches every 3s while the job is in progress.
 */
export function useRegistrationJobStatus(requestId: string | null) {
  return useQuery({
    queryKey: registrationJobsKeys.detail(requestId ?? ''),
    queryFn: () => {
      if (!requestId) {
        throw new Error('requestId is required')
      }
      return nodeRegistrationApi.getRegistrationJobStatus(requestId)
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
export function useDownloadRegistrationCsv() {
  return useMutation<void, Error, string>({
    mutationFn: (s3Path) => downloadCertCsv(s3Path),
  })
}

/**
 * List registration jobs. Auto-refetches every 10s while any job in the current
 * page is still in progress.
 */
export function useRegistrationJobs(params: RegistrationJobsListParams) {
  const credentials = useAuthStore((s) => s.credentials)
  return useQuery({
    ...registrationJobsQueries.list(params),
    enabled: !!credentials,
  })
}
