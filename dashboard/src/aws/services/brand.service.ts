/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { DeleteObjectCommand, PutObjectCommand, type S3Client } from "@aws-sdk/client-s3";
import { getS3Client } from "./s3-client";
import { getServerUrl } from "@/lib/config";
import {
  buildPublishedBrand,
  imageDigest,
  imageRefs,
  parsePublishedBrand,
  readRainmakerBrand,
  serializeBrand,
  type PublishedBrand,
} from "@/utils/rainmaker-brand/rainmaker-brand";

const IMMUTABLE_CACHE_CONTROL = "public, max-age=31536000, immutable";
const NO_CACHE = "no-cache";
const JSON_CONTENT_TYPE = "application/json";
const PNG_CONTENT_TYPE = "image/png";
/** S3 answers 403 for a missing key to anyone without ListBucket, which neither phones nor this page have. */
const NOT_PUBLISHED_STATUSES = [403, 404];
const HOSTED_IMAGE_NAME = /^[0-9a-f]{16}\.png$/;

interface PublicAssetsLocation {
  bucket: string;
  region: string;
  origin: string;
  /** The deployment's brand folder in the shared bucket, e.g. `eu-west-1/brand`. */
  brandPrefix: string;
}

/**
 * Where the brand lives, read off the client outputs URL:
 * `https://rmng-public-assets-<account>.s3.us-east-1.amazonaws.com/eu-west-1/rmng-client-outputs.json`
 * -> bucket `rmng-public-assets-<account>`, region `us-east-1`, brand folder `eu-west-1/brand`.
 */
function publicAssetsLocation(): PublicAssetsLocation {
  const url = new URL(getServerUrl());
  const [bucket, , region] = url.hostname.split(".");
  const outputsKey = url.pathname.slice(1);
  return {
    bucket,
    region,
    origin: url.origin,
    brandPrefix: `${outputsKey.slice(0, outputsKey.lastIndexOf("/"))}/brand`,
  };
}

function brandKey(location: PublicAssetsLocation): string {
  return `${location.brandPrefix}/brand.json`;
}

/** Keys of the images this deployment hosts for a brand; external https:// images are left alone. */
function hostedImageKeys(location: PublicAssetsLocation, brand: PublishedBrand | null): Set<string> {
  const imagesUrl = `${location.origin}/${location.brandPrefix}/images/`;
  const urls = brand ? Object.values(imageRefs(brand.config)) : [];
  return new Set(
    urls
      .filter((url) => url.startsWith(imagesUrl) && HOSTED_IMAGE_NAME.test(url.slice(imagesUrl.length)))
      .map((url) => url.slice(location.origin.length + 1)),
  );
}

/** Phones keep their own copies of the images, so these deletes cannot blank a brand on screen; a failed one only leaves an unused file. */
async function deleteImages(client: S3Client, location: PublicAssetsLocation, keys: string[]): Promise<void> {
  await Promise.allSettled(
    keys.map((key) => client.send(new DeleteObjectCommand({ Bucket: location.bucket, Key: key }))),
  );
}

/** The brand phones download, or null when the deployment has none. */
export async function readPublishedBrand(): Promise<PublishedBrand | null> {
  const location = publicAssetsLocation();
  const response = await fetch(`${location.origin}/${brandKey(location)}`, { cache: "no-store" });
  if (NOT_PUBLISHED_STATUSES.includes(response.status)) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`brand.json returned HTTP ${response.status}`);
  }
  return parsePublishedBrand(await response.json());
}

/** Publishes a .rainmaker brand: images first and `brand.json` last, so a phone never reads a brand whose images are missing, then the previous brand's unused images go. */
export async function publishBrand(file: File): Promise<PublishedBrand> {
  const { brand, images } = await readRainmakerBrand(file);
  const location = publicAssetsLocation();
  const client = getS3Client(location.region);
  const previous = await readPublishedBrand().catch(() => null);

  const imageUrls = new Map<string, string>();
  await Promise.all(
    Array.from(images, async ([ref, bytes]) => {
      const key = `${location.brandPrefix}/images/${await imageDigest(bytes)}.png`;
      await client.send(
        new PutObjectCommand({
          Bucket: location.bucket,
          Key: key,
          Body: bytes,
          ContentType: PNG_CONTENT_TYPE,
          CacheControl: IMMUTABLE_CACHE_CONTROL,
        }),
      );
      imageUrls.set(ref, `${location.origin}/${key}`);
    }),
  );

  const published = await buildPublishedBrand(brand, imageUrls);
  await client.send(
    new PutObjectCommand({
      Bucket: location.bucket,
      Key: brandKey(location),
      Body: serializeBrand(published),
      ContentType: JSON_CONTENT_TYPE,
      CacheControl: NO_CACHE,
    }),
  );
  const kept = hostedImageKeys(location, published);
  await deleteImages(client, location, [...hostedImageKeys(location, previous)].filter((key) => !kept.has(key)));
  return published;
}

/** Removes the brand and its images; phones go back to the default look from their next launches. */
export async function deleteBrand(): Promise<void> {
  const location = publicAssetsLocation();
  const client = getS3Client(location.region);
  const previous = await readPublishedBrand().catch(() => null);
  await client.send(new DeleteObjectCommand({ Bucket: location.bucket, Key: brandKey(location) }));
  await deleteImages(client, location, [...hostedImageKeys(location, previous)]);
}
