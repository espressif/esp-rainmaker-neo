/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { FirmwareUploadResult } from '@/aws/services/firmware-upload.service'

export type UploadState =
  | { step: 'idle' }
  | { step: 'hashing' }
  | { step: 'uploading' }
  | { step: 'success'; result: FirmwareUploadResult }
  | { step: 'error'; message: string }

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`
  }
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

export function stripExtension(filename: string): string {
  const lastDot = filename.lastIndexOf('.')
  return lastDot > 0 ? filename.substring(0, lastDot) : filename
}
