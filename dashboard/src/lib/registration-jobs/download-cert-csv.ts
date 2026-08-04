/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { GetObjectCommand } from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";
import { getS3Client } from "@/aws/services/s3-client";

/** Raised when the cert CSV object no longer exists in S3 (404 on presigned GET). */
export class CsvNotAvailableError extends Error {}

/**
 * Presign and download a registration-job cert CSV. The bucket allows
 * cross-origin GET, so we hit the presigned URL directly to turn a missing
 * object into a catchable 404 instead of navigating to raw S3 error XML.
 */
export async function downloadCertCsv(s3Path: string): Promise<void> {
  const withoutPrefix = s3Path.replace("s3://", "");
  const slashIndex = withoutPrefix.indexOf("/");
  if (slashIndex < 0) {
    throw new Error(`Unexpected S3 path: ${s3Path}`);
  }
  const bucket = withoutPrefix.slice(0, slashIndex);
  const key = withoutPrefix.slice(slashIndex + 1);

  const url = await getSignedUrl(
    getS3Client(),
    new GetObjectCommand({ Bucket: bucket, Key: key }),
    { expiresIn: 3600 },
  );

  const res = await fetch(url);
  if (!res.ok) {
    if (res.status === 404) {
      throw new CsvNotAvailableError();
    }
    throw new Error(`Download failed (${res.status})`);
  }

  const blob = await res.blob();
  const objectUrl = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = objectUrl;
  a.download = key.split("/").pop() || "registration.csv";
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(objectUrl);
}
