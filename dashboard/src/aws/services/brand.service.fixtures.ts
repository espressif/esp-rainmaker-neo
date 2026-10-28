/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { DeleteObjectCommand, type PutObjectCommand } from "@aws-sdk/client-s3";

export const ORIGIN = "https://rmng-public-assets-123456789012.s3.us-east-1.amazonaws.com";
export const OUTPUTS_URL = `${ORIGIN}/eu-west-1/rmng-client-outputs.json`;
export const BRAND_KEY = "eu-west-1/brand/brand.json";
export const IMAGES_PREFIX = "eu-west-1/brand/images/";

/** In-memory bucket: signed writes go through `send`, public reads through `fetch`, which answers 403 for a missing key as S3 does. */
export class FakeBucket {
  objects = new Map<string, { body: string | Uint8Array; cacheControl?: string }>();
  writes: string[] = [];
  readStatus?: number;

  reset() {
    this.objects.clear();
    this.writes = [];
    this.readStatus = undefined;
  }

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
