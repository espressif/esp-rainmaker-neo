/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export interface GetFileUploadUrlRequest {
  file_type: 'node_cert'
  file_name: string
}

export interface GetFileUploadUrlResponse {
  upload_url: string
  s3_path: string
}

export interface CreateBulkRegistrationJobRequest {
  cert_file_s3_path: string
  admin_group_names?: string[]
  admin_parent_group_name?: string
  tags?: string[]
  capabilities?: string[]
}

export interface CreateBulkRegistrationJobResponse {
  status: string
  request_id: string
  message?: string
}

export interface RegistrationJobStatusResponse {
  request_id: string
  user_id?: string
  job_type?: string
  total_nodes: number
  success_count?: number
  failed_count?: number
  created_at?: number
  last_updated_at?: number
  status: string
  message?: string
  admin_group_names?: string[]
  admin_parent_group_name?: string
  tags?: string[]
  cert_file_s3_path?: string
  /**
   * Set only when the job completed with failures and the container's S3 write
   * succeeded. `failed_file_download_url` is a short-lived (15 min) presigned GET
   * minted per status call — the UI presigns `failed_file_s3_path` at click time
   * instead (see `downloadCertCsv`) so a long-open dialog can't hand out a stale
   * URL, and so a deleted object surfaces as a catchable 404.
   */
  failed_file_s3_path?: string
  failed_file_download_url?: string
}

export interface ListRegistrationJobsResponse {
  jobs: RegistrationJobStatusResponse[]
  page_total: number
  next_key?: string
}
