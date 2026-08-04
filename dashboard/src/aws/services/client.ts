/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { IoTClient } from "@aws-sdk/client-iot";
import { CloudFormationClient } from "@aws-sdk/client-cloudformation";
import { useAuthStore } from "@/stores/auth.store";
import { getAwsRegion } from "@/lib/config";

function awsCredentials() {
  const { credentials, isCredentialsValid } = useAuthStore.getState();
  if (!credentials) {throw new Error("No AWS credentials available");}
  if (!isCredentialsValid()) {
    useAuthStore.getState().clearCredentials();
    throw new Error("AWS credentials expired");
  }
  return {
    accessKeyId: credentials.accessKeyId,
    secretAccessKey: credentials.secretAccessKey,
    sessionToken: credentials.sessionToken,
  };
}

export function getIoTClient(): IoTClient {
  return new IoTClient({
    region: getAwsRegion(),
    credentials: awsCredentials(),
  });
}

export function getCloudFormationClient(): CloudFormationClient {
  return new CloudFormationClient({
    region: getAwsRegion(),
    credentials: awsCredentials(),
  });
}
