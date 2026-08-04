/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  CreateStreamCommand,
  CreateJobCommand,
  ListJobsCommand,
  ListJobExecutionsForThingCommand,
  ListJobExecutionsForJobCommand,
  DescribeJobCommand,
  DescribeJobExecutionCommand,
  CancelJobCommand,
  DeleteJobCommand,
  DeleteStreamCommand,
  type JobExecutionStatus,
  type JobStatus,
  type TargetSelection,
} from "@aws-sdk/client-iot";
import { ListObjectsV2Command } from "@aws-sdk/client-s3";
import { getIoTClient } from "./client";
import { getS3Client } from "./s3-client";
import { describeThingGroup, createDynamicThingGroup } from "./thing-group.service";
import { describeThing } from "./thing.service";

export const OTA_JOB_PREFIX = "AFR_OTA-";

/** Strip the AFR_OTA- prefix for display purposes. */
export function stripOtaPrefix(jobId: string): string {
  return jobId.startsWith(OTA_JOB_PREFIX)
    ? jobId.slice(OTA_JOB_PREFIX.length)
    : jobId;
}

export async function listOTAJobs(params?: {
  maxResults?: number;
  nextToken?: string;
  thingGroupName?: string;
  status?: JobStatus;
  targetSelection?: TargetSelection;
}) {
  const client = getIoTClient();
  const response = await client.send(
    new ListJobsCommand({
      maxResults: params?.maxResults,
      nextToken: params?.nextToken,
      ...(params?.thingGroupName && { thingGroupName: params.thingGroupName }),
      ...(params?.status && { status: params.status }),
      ...(params?.targetSelection && { targetSelection: params.targetSelection }),
    })
  );

  const jobs = (response.jobs ?? []).filter((j) =>
    j.jobId?.startsWith(OTA_JOB_PREFIX)
  );

  return {
    jobs,
    nextToken: response.nextToken,
  };
}

export interface ThingJobExecutionSummary {
  jobId: string;
  status?: string;
  queuedAt?: Date;
  startedAt?: Date;
  lastUpdatedAt?: Date;
  executionNumber?: number;
}

export async function listJobExecutionsForThing(params: {
  thingName: string;
  maxResults?: number;
  nextToken?: string;
}): Promise<{ executions: ThingJobExecutionSummary[]; nextToken?: string }> {
  const client = getIoTClient();
  const response = await client.send(
    new ListJobExecutionsForThingCommand({
      thingName: params.thingName,
      maxResults: params.maxResults,
      nextToken: params.nextToken,
    })
  );

  const executions: ThingJobExecutionSummary[] = (
    response.executionSummaries ?? []
  )
    .filter((e) => e.jobId?.startsWith(OTA_JOB_PREFIX))
    .map((e) => ({
      jobId: e.jobId ?? "",
      status: e.jobExecutionSummary?.status,
      queuedAt: e.jobExecutionSummary?.queuedAt,
      startedAt: e.jobExecutionSummary?.startedAt,
      lastUpdatedAt: e.jobExecutionSummary?.lastUpdatedAt,
      executionNumber: e.jobExecutionSummary?.executionNumber,
    }));

  return { executions, nextToken: response.nextToken };
}

export async function describeJob(jobId: string) {
  const client = getIoTClient();
  const response = await client.send(new DescribeJobCommand({ jobId }));
  return response.job;
}

export interface JobExecutionSummary {
  thingName: string;
  status?: string;
  queuedAt?: Date;
  startedAt?: Date;
  lastUpdatedAt?: Date;
  executionNumber?: number;
}

export async function listJobExecutionsForJob(params: {
  jobId: string;
  status?: string;
  maxResults?: number;
  nextToken?: string;
}): Promise<{ executions: JobExecutionSummary[]; nextToken?: string }> {
  const client = getIoTClient();
  const response = await client.send(
    new ListJobExecutionsForJobCommand({
      jobId: params.jobId,
      ...(params.status && { status: params.status as JobExecutionStatus }),
      maxResults: params.maxResults,
      nextToken: params.nextToken,
    })
  );

  const executions: JobExecutionSummary[] = (response.executionSummaries ?? []).map((e) => {
    const thingArn = e.thingArn ?? "";
    const thingName = thingArn.split("/").pop() ?? thingArn;
    return {
      thingName,
      status: e.jobExecutionSummary?.status,
      queuedAt: e.jobExecutionSummary?.queuedAt,
      startedAt: e.jobExecutionSummary?.startedAt,
      lastUpdatedAt: e.jobExecutionSummary?.lastUpdatedAt,
      executionNumber: e.jobExecutionSummary?.executionNumber,
    };
  });

  return { executions, nextToken: response.nextToken };
}

export async function describeJobExecution(params: {
  jobId: string;
  thingName: string;
}) {
  const client = getIoTClient();
  const response = await client.send(
    new DescribeJobExecutionCommand({
      jobId: params.jobId,
      thingName: params.thingName,
    })
  );
  return response.execution;
}

export async function cancelOTAJob(jobId: string) {
  const client = getIoTClient();
  await client.send(new CancelJobCommand({ jobId, force: true }));
}

export async function deleteOTAJob(jobId: string) {
  const client = getIoTClient();
  const streamId = jobId;

  // Cancel first if not already terminal
  try {
    await client.send(new CancelJobCommand({ jobId, force: true }));
  } catch {
    // Job may already be in a terminal state — ignore
  }

  // Delete job
  await client.send(new DeleteJobCommand({ jobId, force: true }));

  // Delete associated stream
  try {
    await client.send(new DeleteStreamCommand({ streamId }));
  } catch {
    // Stream may not exist or already deleted — ignore
  }
}

async function getFirmwareFileSize(bucket: string, key: string): Promise<number> {
  const client = getS3Client();
  const response = await client.send(
    new ListObjectsV2Command({ Bucket: bucket, Prefix: key, MaxKeys: 1 })
  );
  return response.Contents?.[0]?.Size ?? 0;
}

/**
 * Return a lowercase hex MD5 suitable for the rmng_ota `file_md5` field, or
 * undefined if the input is not a plain 32-char MD5 (e.g. a multipart S3 ETag
 * like "<hex>-<n>", which is not the image MD5).
 */
function normalizeFileMd5(value?: string): string | undefined {
  if (!value) {return undefined;}
  const md5 = value.toLowerCase();
  return /^[0-9a-f]{32}$/.test(md5) ? md5 : undefined;
}

export interface CreateOtaJobParams {
  otaUpdateId: string;
  targetType: "group" | "node";
  targetSelection: "SNAPSHOT" | "CONTINUOUS";
  targetName?: string;
  dynamicGroupQuery?: string;
  firmwareKey: string;
  roleArn: string;
  bucket: string;
  fwVersion?: string;
  /**
   * Lowercase hex MD5 of the entire OTA image. When set, the device enables
   * auto-resume of an interrupted download plus an end-to-end MD5 integrity
   * check. Omit to disable both.
   */
  fileMd5?: string;
}

export async function createOTAJob(params: CreateOtaJobParams) {
  let targetArn: string;

  if (params.dynamicGroupQuery) {
    // Create a dynamic group for continuous OTA targeting
    const groupName = `ota-${params.otaUpdateId}`;
    const result = await createDynamicThingGroup({
      thingGroupName: groupName,
      queryString: params.dynamicGroupQuery,
      description: `Auto-created for OTA job ${params.otaUpdateId}`,
    });
    targetArn = result.thingGroupArn ?? "";
    if (!targetArn) {
      throw new Error(`Could not create dynamic group "${groupName}"`);
    }
  } else if (params.targetType === "group") {
    if (!params.targetName) {
      throw new Error("targetName is required for group target type");
    }
    const groupInfo = await describeThingGroup({
      thingGroupName: params.targetName,
    });
    targetArn = groupInfo.thingGroupArn ?? "";
    if (!targetArn) {
      throw new Error(
        `Could not resolve ARN for thing group "${params.targetName}"`
      );
    }
  } else {
    if (!params.targetName) {
      throw new Error("targetName is required for node target type");
    }
    const thingInfo = await describeThing({
      thingName: params.targetName,
    });
    targetArn = thingInfo.thingArn ?? "";
    if (!targetArn) {
      throw new Error(
        `Could not resolve ARN for node "${params.targetName}"`
      );
    }
  }

  const jobId = `${OTA_JOB_PREFIX}${params.otaUpdateId}`;
  const streamId = jobId;
  const fileName = params.firmwareKey.replace(/^ota\//, "");

  // Get file size from S3
  const fileSize = await getFirmwareFileSize(params.bucket, params.firmwareKey);

  const client = getIoTClient();

  // 1. Create IoT stream for S3 file delivery
  await client.send(
    new CreateStreamCommand({
      streamId,
      files: [
        {
          fileId: 0,
          s3Location: {
            bucket: params.bucket,
            key: params.firmwareKey,
          },
        },
      ],
      roleArn: params.roleArn,
    })
  );

  // 2. Build job document with both afr_ota and rmng_ota sections.
  //    file_md5 must be a lowercase hex MD5 of the *whole* image. The S3 ETag
  //    equals that only for single-part uploads; a multipart ETag ("<hex>-<n>")
  //    is not an MD5, so it is dropped — file_md5 stays absent and the device
  //    falls back to a non-resumable download (no integrity check).
  const fileMd5 = normalizeFileMd5(params.fileMd5);

  const jobDocument = JSON.stringify({
    afr_ota: {
      protocols: ["MQTT"],
      streamname: streamId,
      files: [
        {
          filepath: fileName,
          filesize: fileSize,
          fileid: 0,
          certfile: "NA",
          "sig-sha256-ecdsa": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
        },
      ],
    },
    rmng_ota: {
      fw_version: params.fwVersion ?? "",
      ...(fileMd5 ? { file_md5: fileMd5 } : {}),
    },
  });

  // 3. Create IoT job
  return await client.send(
    new CreateJobCommand({
      jobId,
      targets: [targetArn],
      document: jobDocument,
      targetSelection: params.targetSelection,
    })
  );
}
