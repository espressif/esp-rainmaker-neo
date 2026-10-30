/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Reads the `brand/` folder of a .rainmaker file saved from ESP RainMaker Neo App Studio and builds the `brand.json` phones download.

import JSZip from "jszip";
import type { z } from "zod";
import {
  brandDocumentSchema,
  publishedBrandSchema,
  type BrandConfig,
  type BrandDocument,
  type PublishedBrand,
} from "./rainmaker-brand.schema";

export type { BrandConfig, BrandTheme, PublishedBrand } from "./rainmaker-brand.schema";

const MAX_ARCHIVE_ENTRIES = 200;
const MAX_ARCHIVE_BYTES = 25 * 1024 * 1024;
const MAX_IMAGE_BYTES = 2 * 1024 * 1024;
const MAX_IMAGE_SIDE = 2048;
const MAX_BRAND_BYTES = 32 * 1024;
const ARCHIVE_IMAGE = /^brand\/images\/[A-Za-z0-9._-]+\.png$/;
const MS_PER_SECOND = 1000;
const PNG_SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
const PNG_IHDR = "IHDR";
const MANIFEST_PATH = "manifest.json";
const BRAND_JSON_PATH = "brand/brand.json";
const SYMLINK_MODE = 0o120000;
const FILE_TYPE_MASK = 0o170000;

type RainmakerBrandErrorReason = "notRainmaker" | "noBrand" | "invalidBrand" | "invalidImage" | "tooLarge";

/** Why a .rainmaker file cannot be published; `detail` names the field or entry. */
export class RainmakerBrandError extends Error {
  constructor(
    readonly reason: RainmakerBrandErrorReason,
    readonly detail = "",
  ) {
    super(detail ? `${reason}: ${detail}` : reason);
    this.name = "RainmakerBrandError";
  }
}

function parseBrand<T extends z.ZodType>(schema: T, value: unknown, source: string): z.infer<T> {
  const parsed = schema.safeParse(value);
  if (!parsed.success) {
    const [issue] = parsed.error.issues;
    throw new RainmakerBrandError("invalidBrand", `${issue.path.join(".") || source}: ${issue.message}`);
  }
  return parsed.data;
}

/**
 * Checks a brand read back from `<region>/brand/brand.json`.
 * @throws {RainmakerBrandError} with reason `invalidBrand`
 */
export function parsePublishedBrand(value: unknown): PublishedBrand {
  return parseBrand(publishedBrandSchema, value, BRAND_JSON_PATH);
}

interface RainmakerBrand {
  brand: BrandDocument;
  /** Every archive image the brand refers to, keyed by its archive path. */
  images: Map<string, Uint8Array>;
}

/** Every string in a value by field path: `{ brandLogo: { light: "a.png" }, ui: { radius: 8 } }` -> `{ "brandLogo.light": "a.png" }`. */
export function stringFields(value: unknown, path = ""): Record<string, string> {
  if (typeof value === "string") {
    return { [path]: value };
  }
  if (value === null || typeof value !== "object") {
    return {};
  }
  return Object.assign(
    {},
    ...Object.entries(value).map(([key, child]) => stringFields(child, path ? `${path}.${key}` : key)),
  ) as Record<string, string>;
}

/** Every archive image a config refers to, wherever App Studio put it, by field path. */
export function imageRefs(config: BrandConfig): Record<string, string> {
  return Object.fromEntries(Object.entries(stringFields(config)).filter(([, ref]) => ARCHIVE_IMAGE.test(ref)));
}

function isUnsafeEntry(entry: JSZip.JSZipObject): boolean {
  const name = entry.name;
  const mode = Number(entry.unixPermissions ?? 0);
  return (
    name.startsWith("/") ||
    /^[A-Za-z]:/.test(name) ||
    name.includes("\\") ||
    name.split("/").includes("..") ||
    (mode & FILE_TYPE_MASK) === SYMLINK_MODE
  );
}

async function readArchiveJson(zip: JSZip, path: string): Promise<unknown> {
  const entry = zip.file(path);
  if (!entry) {
    throw new RainmakerBrandError("noBrand", path);
  }
  try {
    return JSON.parse(await entry.async("string")) as unknown;
  } catch {
    throw new RainmakerBrandError("invalidBrand", `${path} is not valid JSON`);
  }
}

/** Rejects anything that is not a PNG by its magic bytes, or exceeds the size limits. */
function checkPng(path: string, bytes: Uint8Array): void {
  if (bytes.length > MAX_IMAGE_BYTES) {
    throw new RainmakerBrandError("invalidImage", `${path} is larger than 2 MB`);
  }
  const header = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const isPng =
    bytes.length >= 24 &&
    PNG_SIGNATURE.every((byte, index) => bytes[index] === byte) &&
    String.fromCharCode(...bytes.subarray(12, 16)) === PNG_IHDR;
  if (!isPng) {
    throw new RainmakerBrandError("invalidImage", `${path} is not a PNG`);
  }
  const width = header.getUint32(16);
  const height = header.getUint32(20);
  if (width === 0 || height === 0 || width > MAX_IMAGE_SIDE || height > MAX_IMAGE_SIDE) {
    throw new RainmakerBrandError("invalidImage", `${path} is ${width}x${height} px, more than 2048 px on a side`);
  }
}

async function readImages(zip: JSZip, config: BrandConfig): Promise<Map<string, Uint8Array>> {
  const images = new Map<string, Uint8Array>();
  for (const ref of Object.values(imageRefs(config))) {
    if (images.has(ref)) {
      continue;
    }
    const entry = zip.file(ref);
    if (!entry) {
      throw new RainmakerBrandError("invalidImage", `${ref} is not in the file`);
    }
    const bytes = await entry.async("uint8array");
    checkPng(ref, bytes);
    images.set(ref, bytes);
  }
  return images;
}

/**
 * Reads and validates a .rainmaker file, touching only `manifest.json` and `brand/`.
 * @throws {RainmakerBrandError} naming the first rule the file breaks
 */
export async function readRainmakerBrand(file: File): Promise<RainmakerBrand> {
  if (file.size > MAX_ARCHIVE_BYTES) {
    throw new RainmakerBrandError("notRainmaker", "the file is larger than 25 MB");
  }
  let zip: JSZip;
  try {
    zip = await JSZip.loadAsync(await file.arrayBuffer());
  } catch {
    throw new RainmakerBrandError("notRainmaker");
  }
  const entries = Object.values(zip.files);
  if (entries.length > MAX_ARCHIVE_ENTRIES || entries.some(isUnsafeEntry)) {
    throw new RainmakerBrandError("notRainmaker", "the archive has too many or unsafe entries");
  }

  const manifest = await readArchiveJson(zip, MANIFEST_PATH);
  if ((manifest as { format?: unknown } | null)?.format !== 1) {
    throw new RainmakerBrandError("notRainmaker", `${MANIFEST_PATH} format must be 1`);
  }

  const brand = parseBrand(brandDocumentSchema, await readArchiveJson(zip, BRAND_JSON_PATH), BRAND_JSON_PATH);
  return { brand, images: await readImages(zip, brand.config) };
}

async function sha256Hex(data: Uint8Array | string): Promise<string> {
  const bytes = typeof data === "string" ? new TextEncoder().encode(data) : new Uint8Array(data);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

/** The exact `brand.json` body that is uploaded and that phones download. */
export function serializeBrand(brand: PublishedBrand): string {
  return JSON.stringify(brand, null, 2);
}

/** S3 key name for an image: the first 16 hex characters of its sha256, so an unchanged image is never uploaded twice. */
export async function imageDigest(bytes: Uint8Array): Promise<string> {
  return (await sha256Hex(bytes)).slice(0, 16);
}

/** sha256 over the config with its image URLs, which carry each image's own hash, so anything a phone would show changes it; `publishedAt` is left out. */
export async function brandChecksum(config: BrandConfig): Promise<string> {
  return sha256Hex(JSON.stringify(config));
}

/** A copy of a config with every archive image path swapped for its public URL; every other value stays as written. */
function withImageUrls<T>(value: T, imageUrls: Map<string, string>): T {
  if (typeof value === "string") {
    return (imageUrls.get(value) ?? value) as T;
  }
  if (value === null || typeof value !== "object") {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item: unknown) => withImageUrls(item, imageUrls)) as T;
  }
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, withImageUrls(child, imageUrls)])) as T;
}

/**
 * Builds the published brand from a validated brand.json and the URLs its images were uploaded to; top-level keys other than `config` are not published, as the checksum does not cover them.
 * @throws {RainmakerBrandError} when `brand.json` would be over 32 KB, since every phone downloads it on each launch
 */
export async function buildPublishedBrand(brand: BrandDocument, imageUrls: Map<string, string>): Promise<PublishedBrand> {
  const config = withImageUrls(brand.config, imageUrls);
  const published: PublishedBrand = {
    schemaVersion: brand.schemaVersion,
    checksum: await brandChecksum(config),
    publishedAt: Math.floor(Date.now() / MS_PER_SECOND),
    config,
  };
  if (new TextEncoder().encode(serializeBrand(published)).length > MAX_BRAND_BYTES) {
    throw new RainmakerBrandError("tooLarge");
  }
  return published;
}
