/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  PutObjectCommand,
  GetObjectCommand,
  ListObjectsV2Command,
  GetObjectTaggingCommand,
} from "@aws-sdk/client-s3";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";
import SparkMD5 from "spark-md5";
import { getS3Client } from "./s3-client";
import { getOtaS3Bucket } from "@/lib/config";

export interface FirmwareUploadParams {
  file: File;
  name: string;
  version: string;
  type?: string;
  model?: string;
  platform?: string;
}

export interface FirmwareUploadResult {
  url: string;
  key: string;
  fileSize: number;
  md5: string;
}

export interface FirmwareFileRow {
  key: string;
  name: string;
  size: number;
  lastModified?: Date;
  md5?: string;
  version?: string;
  type?: string;
  model?: string;
  platform?: string;
}

/** Every firmware object lives under this key prefix in the OTA bucket. */
const OTA_KEY_PREFIX = "ota/";

function sanitizeFilename(name: string): string {
  return name.replace(/[^a-zA-Z0-9._-]/g, "_");
}

/** Compute MD5 hash of a file using spark-md5. Returns hex and base64 representations. */
export async function computeMD5(
  file: File
): Promise<{ hex: string; base64: string }> {
  const arrayBuffer = await file.arrayBuffer();
  const spark = new SparkMD5.ArrayBuffer();
  spark.append(arrayBuffer);
  const hex = spark.end(false);

  // Convert hex to base64 for S3 ContentMD5
  const bytePairs = hex.match(/.{2}/g)
  if (!bytePairs) {
    throw new Error('Invalid MD5 hex string')
  }
  const bytes = new Uint8Array(bytePairs.map((byte) => parseInt(byte, 16)))
  const base64 = btoa(String.fromCharCode(...bytes));

  return { hex, base64 };
}

function buildTaggingString(tags: Record<string, string>): string {
  return Object.entries(tags)
    .filter(([, v]) => v)
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`)
    .join("&");
}

export async function uploadFirmware(
  params: FirmwareUploadParams
): Promise<FirmwareUploadResult> {
  const bucket = getOtaS3Bucket();
  if (!bucket) {throw new Error("OTA S3 bucket not configured");}

  const key = `${OTA_KEY_PREFIX}${sanitizeFilename(params.name)}`;
  const client = getS3Client();
  const arrayBuffer = await params.file.arrayBuffer();
  const md5 = await computeMD5(params.file);

  const tags: Record<string, string> = {};
  if (params.version) {tags["fw-version"] = params.version;}
  if (params.type) {tags["fw-type"] = params.type;}
  if (params.model) {tags["fw-model"] = params.model;}
  if (params.platform) {tags["fw-platform"] = params.platform;}

  const tagging = buildTaggingString(tags);

  await client.send(
    new PutObjectCommand({
      Bucket: bucket,
      Key: key,
      Body: new Uint8Array(arrayBuffer),
      ContentType: params.file.type || "application/octet-stream",
      ContentMD5: md5.base64,
      IfNoneMatch: "*",
      ...(tagging ? { Tagging: tagging } : {}),
    })
  );

  const url = `s3://${bucket}/${key}`;
  return { url, key, fileSize: params.file.size, md5: md5.hex };
}

async function fetchTagsForKey(
  client: ReturnType<typeof getS3Client>,
  bucket: string,
  key: string
): Promise<{ version?: string; type?: string; model?: string; platform?: string }> {
  try {
    const response = await client.send(
      new GetObjectTaggingCommand({ Bucket: bucket, Key: key })
    );
    const tagMap: Record<string, string> = {};
    for (const tag of response.TagSet ?? []) {
      if (tag.Key && tag.Value) {tagMap[tag.Key] = tag.Value;}
    }
    return {
      version: tagMap["fw-version"],
      type: tagMap["fw-type"],
      model: tagMap["fw-model"],
      platform: tagMap["fw-platform"],
    };
  } catch {
    return {};
  }
}

export async function listFirmwareFiles(params?: {
  maxKeys?: number;
  continuationToken?: string;
  /** When false, skip the per-object GetObjectTagging calls (version/type/... left undefined). */
  includeTags?: boolean;
  /**
   * Narrows the listing to keys starting with `ota/<namePrefix>`. S3 ListObjectsV2 only
   * supports prefix matching, so this is a case-sensitive "starts with" — not a substring
   * search — and it cannot influence ordering (results stay in lexicographic key order).
   */
  namePrefix?: string;
}): Promise<{ files: FirmwareFileRow[]; nextToken?: string }> {
  const bucket = getOtaS3Bucket();
  if (!bucket) {throw new Error("OTA S3 bucket not configured");}

  const includeTags = params?.includeTags ?? true;
  const prefix = params?.namePrefix
    ? `${OTA_KEY_PREFIX}${params.namePrefix}`
    : OTA_KEY_PREFIX;
  const client = getS3Client();
  const response = await client.send(
    new ListObjectsV2Command({
      Bucket: bucket,
      Prefix: prefix,
      MaxKeys: params?.maxKeys ?? 5,
      ContinuationToken: params?.continuationToken,
    })
  );

  const objects = (response.Contents ?? []).filter(
    (obj) => obj.Key && obj.Key !== OTA_KEY_PREFIX
  );

  const files: FirmwareFileRow[] = (
    await Promise.all(
      objects.map(async (obj) => {
        const key = obj.Key
        if (!key) {
          return null
        }
        const tags = includeTags ? await fetchTagsForKey(client, bucket, key) : {};
        const etag = obj.ETag?.replace(/"/g, "");
        return {
          key,
          name: key.slice(OTA_KEY_PREFIX.length),
          size: obj.Size ?? 0,
          lastModified: obj.LastModified,
          md5: etag,
          ...tags,
        } satisfies FirmwareFileRow;
      })
    )
  ).filter((file) => file !== null);

  return { files, nextToken: response.NextContinuationToken };
}

/**
 * List every firmware file in the OTA bucket by walking all pages of ListObjectsV2.
 * Tags are skipped for speed (the dropdown only needs key/name); fetch the selected
 * file's metadata on demand with {@link getFirmwareFileTags}.
 */
export async function listAllFirmwareFiles(): Promise<FirmwareFileRow[]> {
  const all: FirmwareFileRow[] = [];
  let continuationToken: string | undefined;

  do {
    const { files, nextToken } = await listFirmwareFiles({
      maxKeys: 1000,
      continuationToken,
      includeTags: false,
    });
    all.push(...files);
    continuationToken = nextToken;
  } while (continuationToken);

  return all;
}

/** Fetch metadata tags (version/type/model/platform) for a single firmware key. */
export async function getFirmwareFileTags(
  key: string
): Promise<{ version?: string; type?: string; model?: string; platform?: string }> {
  const bucket = getOtaS3Bucket();
  if (!bucket) {throw new Error("OTA S3 bucket not configured");}

  return fetchTagsForKey(getS3Client(), bucket, key);
}

/** Generate a presigned download URL for a firmware file (valid for 1 hour). */
export async function getFirmwareDownloadUrl(key: string): Promise<string> {
  const bucket = getOtaS3Bucket();
  if (!bucket) {throw new Error("OTA S3 bucket not configured");}

  const client = getS3Client();
  return getSignedUrl(client, new GetObjectCommand({ Bucket: bucket, Key: key }), {
    expiresIn: 3600,
  });
}
