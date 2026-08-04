/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { signedFetch } from '@/api/signed-fetch'
import type {
  GetFileUploadUrlResponse,
  CreateBulkRegistrationJobRequest,
  CreateBulkRegistrationJobResponse,
  RegistrationJobStatusResponse,
  ListRegistrationJobsResponse,
} from './node-registration.types'

const ENDPOINTS = {
  uploadUrls: '/v1/admin/files/upload-urls',
  registrationJobs: '/v1/admin/nodes/registration-jobs',
  registrationJobStatus: (requestId: string) =>
    `/v1/admin/nodes/registration-jobs/${encodeURIComponent(requestId)}`,
} as const

export const nodeRegistrationApi = {
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

  createBulkRegistrationJob: async (
    params: CreateBulkRegistrationJobRequest,
  ): Promise<CreateBulkRegistrationJobResponse> => {
    const response = await signedFetch(
      'POST',
      ENDPOINTS.registrationJobs,
      JSON.stringify(params),
    )
    return response.json() as Promise<CreateBulkRegistrationJobResponse>
  },

  getRegistrationJobStatus: async (
    requestId: string,
  ): Promise<RegistrationJobStatusResponse> => {
    const response = await signedFetch(
      'GET',
      ENDPOINTS.registrationJobStatus(requestId),
    )
    return response.json() as Promise<RegistrationJobStatusResponse>
  },

  listRegistrationJobs: async (
    pageSize?: number,
    startKey?: string,
    status?: string,
  ): Promise<ListRegistrationJobsResponse> => {
    const params = new URLSearchParams()
    if (pageSize) {params.set('page_size', String(pageSize))}
    if (startKey) {params.set('start_key', startKey)}
    if (status) {params.set('status', status)}
    const qs = params.toString()
    const path = ENDPOINTS.registrationJobs + (qs ? `?${qs}` : '')
    const response = await signedFetch('GET', path)
    return response.json() as Promise<ListRegistrationJobsResponse>
  },
} as const
