/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { ListObjectsV2Command, ListObjectsCommand } from "@aws-sdk/client-s3";
import { getS3Client } from "./s3-client";
import { getAwsRegion, getFilesBucket } from "@/lib/config";

/** A single object returned by an S3 list-objects call. */
export interface S3Object {
  key: string;
  size: number;
  lastModified?: Date;
  etag?: string;
}

/**
 * List objects under a prefix in an S3 bucket via the AWS SDK (SigV4-signed,
 * XML parsed by the SDK). Generic counterpart to the firmware-specific
 * `listFirmwareFiles` — any bucket/prefix/region the caller is authorised for.
 *
 * A single request is issued (up to `maxKeys`); pagination is intentionally not
 * followed here. `listType` selects ListObjectsV2 (default) or the legacy
 * ListObjects; both expose the same `Contents` shape we consume.
 */
export async function listS3Objects(params?: {
  bucket?: string;
  prefix?: string;
  maxKeys?: number;
  listType?: 1 | 2;
  region?: string;
}): Promise<S3Object[]> {
  const bucket = params?.bucket ?? getFilesBucket();
  if (!bucket) {
    throw new Error("S3 bucket not configured");
  }

  const prefix = params?.prefix;
  const maxKeys = params?.maxKeys ?? 1000;
  const client = getS3Client(params?.region ?? getAwsRegion());

  const input = { Bucket: bucket, Prefix: prefix, MaxKeys: maxKeys };
  const response =
    params?.listType === 1
      ? await client.send(new ListObjectsCommand(input))
      : await client.send(new ListObjectsV2Command(input));

  return (response.Contents ?? [])
    .filter((obj) => obj.Key && obj.Key !== prefix)
    .map((obj) => ({
      key: obj.Key as string,
      size: obj.Size ?? 0,
      lastModified: obj.LastModified,
      etag: obj.ETag?.replace(/"/g, ""),
    }));
}
