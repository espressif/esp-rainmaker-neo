/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { LambdaClient } from "@aws-sdk/client-lambda";
import { SESv2Client } from "@aws-sdk/client-sesv2";
import { SNSClient } from "@aws-sdk/client-sns";
import { getAwsRegion } from "@/lib/config";
import type { ScopedAwsCredentials } from "@/api/post-deployment/post-deployment.types";

/**
 * Clients built from a stack's scoped, read-only STS credentials — as opposed to
 * {@link ./client.ts}, which builds from the signed-in admin's identity-pool
 * credentials.
 *
 * One factory per service rather than one bag of all three: the two stacks are
 * scoped differently (espuser vends the SES and SNS reads, rmng the Lambda one),
 * so handing out an SES client built from rmng credentials would only ever
 * produce an access-denied error at call time.
 */
function scopedConfig(creds: ScopedAwsCredentials) {
  return {
    region: getAwsRegion(),
    credentials: {
      accessKeyId: creds.accessKeyId,
      secretAccessKey: creds.secretAccessKey,
      sessionToken: creds.sessionToken,
    },
  };
}

export function getScopedSesClient(creds: ScopedAwsCredentials): SESv2Client {
  return new SESv2Client(scopedConfig(creds));
}

export function getScopedSnsClient(creds: ScopedAwsCredentials): SNSClient {
  return new SNSClient(scopedConfig(creds));
}

export function getScopedLambdaClient(
  creds: ScopedAwsCredentials,
): LambdaClient {
  return new LambdaClient(scopedConfig(creds));
}
