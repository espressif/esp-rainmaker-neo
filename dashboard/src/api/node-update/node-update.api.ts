/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { signedFetch } from '@/api/signed-fetch'
import type {
  GetFileUploadUrlResponse,
  CreateBulkUpdateJobRequest,
  CreateBulkUpdateJobResponse,
  UpdateJobStatusResponse,
  ListUpdateJobsResponse,
} from './node-update.types'

const ENDPOINTS = {
  uploadUrls: '/v1/admin/files/upload-urls',
  updateJobs: '/v1/admin/nodes/update-jobs',
  updateJobStatus: (requestId: string) =>
    `/v1/admin/nodes/update-jobs/${encodeURIComponent(requestId)}`,
} as const

export const nodeUpdateApi = {
  getFileUploadUrl: async (fileName: string): Promise<GetFileUploadUrlResponse> => {
    const response = await signedFetch(
      'POST',
      ENDPOINTS.uploadUrls,
      JSON.stringify({ file_type: 'node_cert', file_name: fileName }),
    )
    return response.json() as Promise<GetFileUploadUrlResponse>
  },

  uploadFileToS3: async (uploadUrl: string, file: File): Promise<void> => {
    // The presigned URL is signed with If-None-Match: * (prevents overwriting existing files).
    // We must include this header so the signature matches.
    const response = await fetch(uploadUrl, {
      method: 'PUT',
      headers: {
        'If-None-Match': '*',
      },
      body: file,
    })
    if (!response.ok) {
      throw new Error(`S3 upload failed: ${response.status}`)
    }
  },

  createBulkUpdateJob: async (
    params: CreateBulkUpdateJobRequest,
  ): Promise<CreateBulkUpdateJobResponse> => {
    const response = await signedFetch(
      'POST',
      ENDPOINTS.updateJobs,
      JSON.stringify(params),
    )
    return response.json() as Promise<CreateBulkUpdateJobResponse>
  },

  getUpdateJobStatus: async (
    requestId: string,
  ): Promise<UpdateJobStatusResponse> => {
    const response = await signedFetch(
      'GET',
      ENDPOINTS.updateJobStatus(requestId),
    )
    return response.json() as Promise<UpdateJobStatusResponse>
  },

  listUpdateJobs: async (
    pageSize?: number,
    startKey?: string,
    status?: string,
  ): Promise<ListUpdateJobsResponse> => {
    const params = new URLSearchParams()
    if (pageSize) {params.set('page_size', String(pageSize))}
    if (startKey) {params.set('start_key', startKey)}
    if (status) {params.set('status', status)}
    const qs = params.toString()
    const path = ENDPOINTS.updateJobs + (qs ? `?${qs}` : '')
    const response = await signedFetch('GET', path)
    return response.json() as Promise<ListUpdateJobsResponse>
  },
} as const
