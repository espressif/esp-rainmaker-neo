/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { GetObjectCommand, ListObjectsV2Command } from '@aws-sdk/client-s3'
import i18n from '@/i18n/config'
import { getS3Client } from '@/aws/services/s3-client'
import { getDeployedStacks } from '@/lib/config'
import { useEnterpriseStore } from '../enterprise.store'
import { clearCachedEnterpriseTheme } from '../theme-cache'
import { enterpriseNamespace } from '../enterprise.types'
import type { RegisteredPlugin } from '../enterprise.types'
import { featureManifestSchema } from './manifest.schema'
import type { ManifestFeature } from './manifest.schema'
import { sdkRangeSatisfied } from './sdk-range'
import { HOST_SDK_VERSION, SHARED_MODULE_KEYS, installSharedScope } from './shared-scope'
import { MANIFEST_PUBLIC_KEY_SPKI_B64 } from './manifest-public-key'
import {
  ENTERPRISE_ASSETS_BUCKET,
  ENTERPRISE_ASSETS_REGION,
  ENTERPRISE_FEATURES_PREFIX,
} from './enterprise-bucket'
import { sha384Integrity, verifyDetachedJws } from './verify'

/**
 * The enterprise plugin loader (spec: dashboard_modules/docs/enterprise-features-runtime-loading.md
 * in the enterprise superproject).
 * Runs once per session, after the credentials gate in home.tsx. It never blocks the app: the
 * community UI renders regardless, and features appear as they finish loading.
 *
 * Features live in an Espressif-owned central bucket, one directory per feature, discovered by
 * listing. Entitlement is the bucket policy itself: no read grant → AccessDenied → silent
 * community mode. The three community signals are deliberately distinct from per-feature
 * failures — a listed feature that fails verification is skipped *loudly*, never silently.
 */

let started = false

export async function ensureEnterpriseLoaded(): Promise<void> {
  if (started) {
    return
  }
  started = true

  const store = useEnterpriseStore.getState()

  if (import.meta.env.DEV) {
    const devUrl = new URLSearchParams(window.location.search).get('enterpriseDevUrl')
    if (devUrl) {
      await loadDevPlugin(devUrl)
      return
    }
  }

  if (!MANIFEST_PUBLIC_KEY_SPKI_B64) {
    setCommunityStatus()
    return
  }

  store.setStatus('loading')
  try {
    const featureIds = await listFeatureIds()
    if (featureIds.length === 0) {
      // Entitled account but nothing published yet — indistinguishable from community on purpose.
      setCommunityStatus()
      return
    }
    await Promise.all(featureIds.map((id) => loadFeature(id)))
    store.setStatus('ready')
  } catch (error) {
    if (isCommunitySignal(error)) {
      setCommunityStatus()
    } else {
      // Includes ExpiredToken*: the session keeper refreshes creds, and a reload retries. Never downgrade to community on a transient failure — that would hide entitled features silently.
      console.error('[enterprise] loading failed:', error)
      store.setStatus('error')
    }
  }
}

/**
 * Community mode also drops any cached pre-paint theming (see `theme-cache.ts`), so a
 * deployment whose entitlement was revoked stops re-applying a stale theme on its next load.
 * Deliberately not called on 'error': transient failures never demote to community.
 */
function setCommunityStatus(): void {
  clearCachedEnterpriseTheme()
  useEnterpriseStore.getState().setStatus('community')
}

/** Discover feature directories: `admin-dashboard/features/<id>/` common prefixes. */
async function listFeatureIds(): Promise<string[]> {
  const client = getS3Client(ENTERPRISE_ASSETS_REGION)
  const response = await client.send(new ListObjectsV2Command({
    Bucket: ENTERPRISE_ASSETS_BUCKET,
    Prefix: ENTERPRISE_FEATURES_PREFIX,
    Delimiter: '/',
  }))
  return (response.CommonPrefixes ?? [])
    .map((p) => p.Prefix?.slice(ENTERPRISE_FEATURES_PREFIX.length).replace(/\/$/, '') ?? '')
    .filter(Boolean)
}

/**
 * Fetch and verify one feature directory's signed manifest. Any failure here means *this
 * feature* is skipped with a console error; it never demotes the whole session to community —
 * silence is reserved for "no access at all".
 */
async function fetchVerifiedManifest(featureId: string): Promise<ManifestFeature | null> {
  const prefix = `${ENTERPRISE_FEATURES_PREFIX}${featureId}/`
  const [manifestBytes, jws] = await Promise.all([
    getObjectBytes(`${prefix}manifest.json`),
    getObjectBytes(`${prefix}manifest.json.jws`).then((bytes) =>
      new TextDecoder().decode(bytes),
    ),
  ])
  const valid = await verifyDetachedJws(manifestBytes, jws, MANIFEST_PUBLIC_KEY_SPKI_B64)
  if (!valid) {
    console.error(`[enterprise] "${featureId}" manifest signature verification failed — refusing to load it`)
    return null
  }
  const parsed = featureManifestSchema.safeParse(
    JSON.parse(new TextDecoder().decode(manifestBytes)),
  )
  if (!parsed.success) {
    console.error(`[enterprise] "${featureId}" manifest schema mismatch:`, parsed.error)
    return null
  }
  if (parsed.data.id !== featureId) {
    // A signed manifest is only valid in its own directory; a copy elsewhere is a replay.
    console.error(`[enterprise] manifest id "${parsed.data.id}" does not match its directory "${featureId}" — refusing to load it`)
    return null
  }
  return parsed.data
}

async function loadFeature(featureId: string): Promise<void> {
  const { upsertFeature } = useEnterpriseStore.getState()

  try {
    const entry = await fetchVerifiedManifest(featureId)
    if (!entry) {
      return
    }

    if (!sdkRangeSatisfied(entry.sdk, HOST_SDK_VERSION)) {
      upsertFeature({ entry, status: 'incompatible', components: {} })
      return
    }
    const missingModule = (entry.sharedModules ?? []).find(
      (key) => !SHARED_MODULE_KEYS.includes(key),
    )
    if (missingModule) {
      console.error(`[enterprise] "${entry.id}" needs shared module "${missingModule}" this host does not provide`)
      upsertFeature({ entry, status: 'incompatible', components: {} })
      return
    }
    const deployedStacks = getDeployedStacks()
    if ((entry.requiresBackend ?? []).some((stack) => !deployedStacks.includes(stack))) {
      upsertFeature({ entry, status: 'unavailable', components: {} })
      return
    }

    const bytes = await getObjectBytes(
      `${ENTERPRISE_FEATURES_PREFIX}${featureId}/${entry.bundle}`,
    )
    if ((await sha384Integrity(bytes)) !== entry.integrity) {
      console.error(`[enterprise] "${entry.id}" bundle hash mismatch — refusing to run it`)
      upsertFeature({ entry, status: 'unavailable', components: {} })
      return
    }
    const plugin = await importBundle(bytes)
    if (!plugin) {
      console.error(`[enterprise] "${entry.id}" bundle never called __RMNG_ENTERPRISE_REGISTER__`)
      upsertFeature({ entry, status: 'unavailable', components: {} })
      return
    }
    registerPluginI18n(entry.id, plugin)
    upsertFeature({ entry, status: 'loaded', components: plugin.components })
  } catch (error) {
    console.error(`[enterprise] loading "${featureId}" failed:`, error)
  }
}

/**
 * DEV-only escape hatch (`?enterpriseDevUrl=`): treat the URL as an unsigned single-feature
 * release, taking the contributions from the plugin's own registration. Signature and hash
 * checks are deliberately skipped — this path does not exist in production builds.
 */
async function loadDevPlugin(devUrl: string): Promise<void> {
  const store = useEnterpriseStore.getState()
  store.setStatus('loading')
  try {
    const res = await fetch(devUrl, { cache: 'no-store' })
    if (!res.ok) {
      throw new Error(`enterpriseDevUrl HTTP ${res.status}`)
    }
    const plugin = await importBundle(new Uint8Array(await res.arrayBuffer()))
    if (!plugin) {
      throw new Error('dev plugin never called __RMNG_ENTERPRISE_REGISTER__')
    }
    const entry: ManifestFeature = {
      schemaVersion: 1,
      id: plugin.id,
      version: plugin.version ?? '0.0.0-dev',
      sdk: plugin.sdk ?? `^${HOST_SDK_VERSION}`,
      bundle: devUrl,
      integrity: 'dev',
      contributes: plugin.contributes ?? {},
    }
    registerPluginI18n(entry.id, plugin)
    store.upsertFeature({ entry, status: 'loaded', components: plugin.components })
    store.setStatus('ready')
  } catch (error) {
    console.error('[enterprise] dev plugin load failed:', error)
    store.setStatus('error')
  }
}

/**
 * Bundles register through the single `__RMNG_ENTERPRISE_REGISTER__` global, so two imports
 * must never be in flight at once: a concurrent load would overwrite the callback and its
 * `finally` would clear the global while the other module is still evaluating. Features load
 * concurrently (fetch + verify), but this queue serializes the import/registration step.
 */
let importQueue: Promise<unknown> = Promise.resolve()

function importBundle(bytes: Uint8Array): Promise<RegisteredPlugin | null> {
  const run = async (): Promise<RegisteredPlugin | null> => {
    installSharedScope()
    let registered: RegisteredPlugin | null = null
    globalThis.__RMNG_ENTERPRISE_REGISTER__ = (plugin) => {
      registered = plugin
    }
    const url = URL.createObjectURL(
      new Blob([bytes.buffer as ArrayBuffer], { type: 'text/javascript' }),
    )
    try {
      await import(/* @vite-ignore */ url)
    } finally {
      URL.revokeObjectURL(url)
      globalThis.__RMNG_ENTERPRISE_REGISTER__ = undefined
    }
    return registered
  }
  const result = importQueue.then(run, run)
  importQueue = result.then(
    () => undefined,
    () => undefined,
  )
  return result
}

function registerPluginI18n(featureId: string, plugin: RegisteredPlugin): void {
  const ns = enterpriseNamespace(featureId)
  for (const [lng, bundle] of Object.entries(plugin.i18n ?? {})) {
    i18n.addResourceBundle(lng, ns, bundle, true, true)
  }
}

async function getObjectBytes(key: string): Promise<Uint8Array> {
  const client = getS3Client(ENTERPRISE_ASSETS_REGION)
  const response = await client.send(new GetObjectCommand({
    Bucket: ENTERPRISE_ASSETS_BUCKET,
    Key: key,
  }))
  if (!response.Body) {
    throw new Error(`empty S3 body for ${key}`)
  }
  return response.Body.transformToByteArray()
}

/** AccessDenied / missing objects mean "not an enterprise-entitled deployment" — silent community mode, not an error. */
function isCommunitySignal(error: unknown): boolean {
  const name = error instanceof Error ? error.name : ''
  return ['AccessDenied', 'NoSuchKey', 'NoSuchBucket', 'Forbidden', 'NotFound', '403'].includes(name)
}
