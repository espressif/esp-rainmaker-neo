/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ReactNode } from "react"
import type { S3Object } from "@/aws/services/s3.service"

export interface S3Option {
  value: string
  label: string
  description?: string
}

export interface S3ListObjectsSelectorProps {
  value?: string | string[]
  onSelect: (value: string | string[] | undefined, objects?: S3Object[]) => void
  onError: (error: Error) => void
  /** Bucket to list. Defaults to the rmng-files bucket (`getFilesBucket()`). */
  bucket?: string
  prefix?: string
  maxKeys?: number
  /** ListObjectsV2 (default) or the legacy ListObjects. */
  listType?: 1 | 2
  /** Region of the target bucket. Defaults to the deployment region. */
  region?: string
  multiple?: boolean
  /** Map an S3 object to a select option. Default: value=key, label=key with prefix stripped. */
  formatOption?: (object: S3Object) => S3Option
  /**
   * Fully custom dropdown rows. Receives the mapped option plus the source S3
   * object (when resolvable by key), so consumers can render size/date/etc.
   */
  renderOption?: (option: S3Option, object?: S3Object) => ReactNode
  label?: ReactNode
  placeholder?: string
  /**
   * When true, once the object list finishes loading, resolve the current
   * `value` to its source object(s) and emit `onSelect(value, [object])` so a
   * preset value (e.g. from a deep link) gains full selection semantics. If the
   * value isn't found in the list, emit `onSelect(undefined)` to clear it.
   * Runs once; an empty `value` is a no-op.
   */
  resolveValueOnLoad?: boolean
}
