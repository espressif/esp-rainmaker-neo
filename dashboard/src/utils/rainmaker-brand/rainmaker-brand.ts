/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

// Reads the `brand/` folder of a .rainmaker file saved from ESP RainMaker Neo App Studio and builds the `brand.json` phones download.

import JSZip from "jszip";
import { z } from "zod";

const MAX_ARCHIVE_ENTRIES = 200;
const MAX_ARCHIVE_BYTES = 25 * 1024 * 1024;
const MAX_IMAGE_BYTES = 2 * 1024 * 1024;
const MAX_IMAGE_SIDE = 2048;
const MAX_BRAND_BYTES = 16 * 1024;
const PNG_SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a];
const PNG_IHDR = "IHDR";
const MANIFEST_PATH = "manifest.json";
const BRAND_JSON_PATH = "brand/brand.json";
const ARCHIVE_IMAGE = /^brand\/images\/[A-Za-z0-9._-]+\.png$/;
const COLOR_ROLE = /^[A-Za-z][A-Za-z0-9_]{0,40}$/;
const COLOR_HEX = /^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$/;
const COLOR_RGBA = /^rgba\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d*\.?\d+)\s*\)$/;
const FIRST_PRINTABLE_CHAR = 0x20;
const DELETE_CHAR = 0x7f;
const SYMLINK_MODE = 0o120000;
const FILE_TYPE_MASK = 0o170000;
export const WORDMARK_HIDDEN = "hidden";

const GLOSSARY_KEYS = [
  "device", "devices", "room", "rooms", "group", "groups",
  "automation", "automations", "schedule", "schedules", "scene", "scenes",
] as const;

const ASSET_KEYS = [
  "light-1", "light-3", "socket", "switch", "fan", "temperature", "sensor", "humidity-sensor",
  "occupancy-sensor", "illuminance-sensor", "door-lock", "ai-assistant", "camera", "rvc",
  "matter-controller", "home", "room", "network", "wifi", "chat-icon",
] as const;

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

function hasControlChar(value: string): boolean {
  return Array.from(value).some((char) => {
    const code = char.charCodeAt(0);
    return code < FIRST_PRINTABLE_CHAR || code === DELETE_CHAR;
  });
}

function isColor(value: string): boolean {
  if (COLOR_HEX.test(value)) {
    return true;
  }
  const match = COLOR_RGBA.exec(value);
  if (!match) {
    return false;
  }
  const [, r, g, b, a] = match;
  return [r, g, b].every((channel) => Number(channel) <= 255) && Number(a) <= 1;
}

const text = (max: number) =>
  z.string().max(max).refine((value) => !hasControlChar(value), "contains a control character");

const brandConfigSchema = z.strictObject({
  whitelabelSchema: z.literal(1),
  generatedBy: text(200).optional(),
  mode: z.enum(["light", "dark"]),
  appName: text(80),
  tagline: text(120).optional(),
  company: text(120).optional(),
  logoUrl: z.string().optional(),
  logoUrlDark: z.string().optional(),
  wordmark: z.string().optional(),
  logoScale: z.number().min(0.5).max(2).optional(),
  radius: z.number().int().min(0).max(32).optional(),
  glossary: z.partialRecord(z.enum(GLOSSARY_KEYS), text(40)).optional(),
  assets: z.partialRecord(z.enum(ASSET_KEYS), z.string()).optional(),
  colors: z
    .partialRecord(
      z.enum(["light", "dark"]),
      z.record(z.string().regex(COLOR_ROLE), z.string().refine(isColor, "is not #RRGGBB, #RRGGBBAA or rgba(r, g, b, a)")),
    )
    .optional(),
});

const brandJsonSchema = z.strictObject({
  schema: z.literal(1),
  generatedBy: text(200).optional(),
  generatedAt: text(200).optional(),
  appearance: z.enum(["light", "dark", "both"]),
  config: brandConfigSchema,
  warnings: z.array(z.string()).optional(),
});

const publishedBrandSchema = z.strictObject({
  schema: z.literal(1),
  checksum: z.string().regex(/^[0-9a-f]{64}$/),
  publishedAt: text(40),
  appearance: brandJsonSchema.shape.appearance,
  config: brandConfigSchema,
});

type BrandJson = z.infer<typeof brandJsonSchema>;
export type BrandConfig = BrandJson["config"];
export type BrandMode = BrandConfig["mode"];
/** The brand as published at `<region>/brand/brand.json`. */
export type PublishedBrand = z.infer<typeof publishedBrandSchema>;

/**
 * Checks a brand read back from `<region>/brand/brand.json`.
 * @throws {RainmakerBrandError} with reason `invalidBrand`
 */
export function parsePublishedBrand(value: unknown): PublishedBrand {
  const parsed = publishedBrandSchema.safeParse(value);
  if (!parsed.success) {
    const [issue] = parsed.error.issues;
    throw new RainmakerBrandError("invalidBrand", `${issue.path.join(".")}: ${issue.message}`);
  }
  return parsed.data;
}

interface RainmakerBrand {
  brand: BrandJson;
  /** Every archive image the brand refers to, keyed by its archive path. */
  images: Map<string, Uint8Array>;
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

function checkExternalImage(field: string, value: string): void {
  let url: URL | null = null;
  try {
    url = new URL(value);
  } catch {
    url = null;
  }
  if (url?.protocol !== "https:" || !url.host || hasControlChar(value) || value.includes(" ")) {
    throw new RainmakerBrandError("invalidBrand", `${field} must be brand/images/<name>.png or an https:// URL`);
  }
}

/** Every image reference in a config by field: `{ logoUrl: "brand/images/logo.png", "assets.fan": "…" }`; a hidden wordmark is not one. */
export function imageRefs(config: BrandConfig): Record<string, string> {
  const refs: Record<string, string> = {};
  if (config.logoUrl) {
    refs.logoUrl = config.logoUrl;
  }
  if (config.logoUrlDark) {
    refs.logoUrlDark = config.logoUrlDark;
  }
  if (config.wordmark && config.wordmark !== WORDMARK_HIDDEN) {
    refs.wordmark = config.wordmark;
  }
  for (const [key, value] of Object.entries(config.assets ?? {})) {
    refs[`assets.${key}`] = value;
  }
  return refs;
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

  const parsed = brandJsonSchema.safeParse(await readArchiveJson(zip, BRAND_JSON_PATH));
  if (!parsed.success) {
    const [issue] = parsed.error.issues;
    throw new RainmakerBrandError("invalidBrand", `${issue.path.join(".") || BRAND_JSON_PATH}: ${issue.message}`);
  }

  const images = new Map<string, Uint8Array>();
  for (const [field, ref] of Object.entries(imageRefs(parsed.data.config))) {
    if (!ARCHIVE_IMAGE.test(ref)) {
      checkExternalImage(field, ref);
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
  return { brand: parsed.data, images };
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

/** sha256 over the appearance and the config with its image URLs, which carry each image's own hash, so anything a phone would show changes it. */
export async function brandChecksum(appearance: BrandJson["appearance"], config: BrandConfig): Promise<string> {
  return sha256Hex(JSON.stringify({ appearance, config }));
}

/** The config with every archive image path replaced by its public URL; a hidden wordmark and https:// images stay as they are. */
function withImageUrls(config: BrandConfig, imageUrls: Map<string, string>): BrandConfig {
  const published: BrandConfig = { ...config };
  for (const field of ["logoUrl", "logoUrlDark", "wordmark"] as const) {
    const ref = config[field];
    if (ref && imageUrls.has(ref)) {
      published[field] = imageUrls.get(ref);
    }
  }
  if (config.assets) {
    published.assets = Object.fromEntries(
      Object.entries(config.assets).map(([key, ref]) => [key, imageUrls.get(ref) ?? ref]),
    );
  }
  return published;
}

/**
 * Builds the published brand from a validated brand.json and the URLs its images were uploaded to.
 * @throws {RainmakerBrandError} when `brand.json` would be over 16 KB, since every phone downloads it on each launch
 */
export async function buildPublishedBrand(brand: BrandJson, imageUrls: Map<string, string>): Promise<PublishedBrand> {
  const config = withImageUrls(brand.config, imageUrls);
  const published: PublishedBrand = {
    schema: 1,
    checksum: await brandChecksum(brand.appearance, config),
    publishedAt: new Date().toISOString().replace(/\.\d{3}Z$/, "Z"),
    appearance: brand.appearance,
    config,
  };
  if (new TextEncoder().encode(serializeBrand(published)).length > MAX_BRAND_BYTES) {
    throw new RainmakerBrandError("tooLarge");
  }
  return published;
}
