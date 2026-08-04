/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useMemo } from "react";
import { LambdaClient } from "@aws-sdk/client-lambda";
import { SESv2Client } from "@aws-sdk/client-sesv2";
import { SNSClient } from "@aws-sdk/client-sns";
import { getAwsRegion } from "@/lib/config";
import { useAdminCreds } from "@/api/post-deployment/admin-creds.queries";
import type { ScopedAwsCredentials, StepStack } from "@/api/post-deployment/post-deployment.types";

type ClientCtor<T> = new (config: {
  region: string;
  credentials: { accessKeyId: string; secretAccessKey: string; sessionToken: string };
}) => T;

function build<T>(Ctor: ClientCtor<T>, creds: ScopedAwsCredentials): T {
  return new Ctor({
    region: getAwsRegion(),
    credentials: {
      accessKeyId: creds.accessKeyId,
      secretAccessKey: creds.secretAccessKey,
      sessionToken: creds.sessionToken,
    },
  });
}

/**
 * Returns memoized AWS SDK clients built from a stack's scoped credentials — espuser vends the
 * SES and SMS sandbox reads, rmng the Lambda concurrency read. Both sets of credentials are
 * read-only, so nothing built here can change an account setting. `isLoading`/`error` surface
 * the underlying credentials query so pages can render skeleton and error states.
 */
export function useAwsClients(stack: StepStack, enabled = true) {
  const { data: creds, isLoading, error, refetch } = useAdminCreds(stack, enabled);

  const clients = useMemo(() => {
    if (!creds) {
      return null;
    }
    return {
      ses: build(SESv2Client, creds),
      sns: build(SNSClient, creds),
      lambda: build(LambdaClient, creds),
    };
  }, [creds]);

  return { clients, isLoading, error, refetch };
}
