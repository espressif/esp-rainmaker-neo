/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { S3Client } from "@aws-sdk/client-s3";
import { useAuthStore } from "@/stores/auth.store";
import { getAwsRegion } from "@/lib/config";

export function getS3Client(region: string = getAwsRegion()): S3Client {
  const credentials = useAuthStore.getState().credentials;
  if (!credentials) {throw new Error("No AWS credentials available");}
  return new S3Client({
    region,
    credentials: {
      accessKeyId: credentials.accessKeyId,
      secretAccessKey: credentials.secretAccessKey,
      sessionToken: credentials.sessionToken,
    },
  });
}
