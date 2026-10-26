/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { beforeEach, describe, expect, it, vi } from "vitest";
import { DeleteObjectCommand, type PutObjectCommand } from "@aws-sdk/client-s3";
import { deleteBrand, publishBrand, readPublishedBrand } from "./brand.service";
import { brandJson, FAN, png, rainmaker, VALID } from "@/utils/rainmaker-brand/rainmaker-brand.fixtures";

const ORIGIN = "https://rmng-public-assets-123456789012.s3.us-east-1.amazonaws.com";
const BRAND_KEY = "eu-west-1/brand/brand.json";
const IMAGES_PREFIX = "eu-west-1/brand/images/";
/** VALID gives the logo and the fan identical bytes, which would share one hosted image. */
const ARCHIVE = { ...VALID, [FAN]: png(8, 8) };

/** In-memory bucket: signed writes go through `send`, public reads through `fetch`, which answers 403 for a missing key as S3 does. */
class FakeBucket {
  objects = new Map<string, { body: string | Uint8Array; cacheControl?: string }>();
  writes: string[] = [];
  readStatus?: number;

  send(command: PutObjectCommand | DeleteObjectCommand) {
    const Key = command.input.Key as string;
    this.writes.push(`${command instanceof DeleteObjectCommand ? "delete" : "put"} ${Key}`);
    if (command instanceof DeleteObjectCommand) {
      this.objects.delete(Key);
    } else {
      const { Body, CacheControl } = command.input;
      this.objects.set(Key, { body: Body as string | Uint8Array, cacheControl: CacheControl });
    }
    return Promise.resolve({});
  }

  fetch = (url: string) => {
    const object = this.objects.get(url.slice(ORIGIN.length + 1));
    const status = this.readStatus ?? (object ? 200 : 403);
    return Promise.resolve(new Response(status === 200 ? (object?.body as string) : null, { status }));
  };

  images(): string[] {
    return Array.from(this.objects.keys()).filter((key) => key.startsWith(IMAGES_PREFIX)).sort();
  }
}

const bucket = new FakeBucket();

vi.mock("./s3-client", () => ({ getS3Client: () => bucket }));
vi.mock("@/lib/config", () => ({
  getServerUrl: () => `${ORIGIN}/eu-west-1/rmng-client-outputs.json`,
}));

beforeEach(() => {
  bucket.objects.clear();
  bucket.writes = [];
  bucket.readStatus = undefined;
  vi.stubGlobal("fetch", bucket.fetch);
});

describe("publishBrand", () => {
  it("uploads every image before brand.json, which phones always revalidate", async () => {
    const brand = await publishBrand(await rainmaker(ARCHIVE));

    expect(bucket.writes.at(-1)).toBe(`put ${BRAND_KEY}`);
    expect(bucket.objects.get(BRAND_KEY)?.cacheControl).toBe("no-cache");
    expect(bucket.images()).toHaveLength(2);
    expect(brand.config.logoUrl).toMatch(new RegExp(`^${ORIGIN}/${IMAGES_PREFIX}[0-9a-f]{16}\\.png$`));
    await expect(readPublishedBrand()).resolves.toEqual(brand);
  });

  it("removes only the previous brand's images the new one does not use", async () => {
    const first = await publishBrand(await rainmaker(ARCHIVE));
    const replaced = await publishBrand(
      await rainmaker({ ...ARCHIVE, "brand/brand.json": brandJson({ assets: { fan: "https://cdn.example.com/fan.png" } }) }),
    );

    expect(replaced.config.logoUrl).toBe(first.config.logoUrl);
    expect(bucket.images()).toEqual([first.config.logoUrl?.slice(ORIGIN.length + 1)]);
    expect(replaced.config.assets).toEqual({ fan: "https://cdn.example.com/fan.png" });
  });
});

describe("readPublishedBrand", () => {
  it("is null when the deployment has no brand", async () => {
    await expect(readPublishedBrand()).resolves.toBeNull();
  });

  it("throws when the bucket fails", async () => {
    bucket.readStatus = 500;

    await expect(readPublishedBrand()).rejects.toThrow("HTTP 500");
  });
});

describe("deleteBrand", () => {
  it("removes brand.json, then its images", async () => {
    await publishBrand(await rainmaker(ARCHIVE));
    bucket.writes = [];

    await deleteBrand();

    expect(bucket.writes[0]).toBe(`delete ${BRAND_KEY}`);
    expect(bucket.objects.size).toBe(0);
    await expect(readPublishedBrand()).resolves.toBeNull();
  });
});
