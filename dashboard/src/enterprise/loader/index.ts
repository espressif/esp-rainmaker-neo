/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { GetObjectCommand } from '@aws-sdk/client-s3'
import i18n from '@/i18n/config'
import { getS3Client } from '@/aws/services/s3-client'
import {
  getDeployedStacks,
  getEnterprisePluginsBucket,
  getStackReleaseVersions,
} from '@/lib/config'
import { useEnterpriseStore } from '../enterprise.store'
import type { EnterpriseFeature, FeatureStatus } from '../enterprise.store'
import { enterpriseNamespace } from '../enterprise.types'
import { isKnownExtensionSlot } from '../slots'
import type { RegisteredPlugin } from '../enterprise.types'
import { pluginIndexSchema } from './manifest.schema'
import type { ManifestFeature, PluginIndex } from './manifest.schema'
import { findReleaseMismatch } from './release-version'
import { sdkRangeSatisfied } from './sdk-range'
import { HOST_SDK_VERSION, SHARED_MODULE_KEYS, installSharedScope } from './shared-scope'
import { sha384Integrity } from './verify'

/**
 * The enterprise plugin loader (spec: dashboard_modules/docs/enterprise-features-runtime-loading.md in the enterprise superproject). Imported only when the deployment names a plugins bucket, so a failure here is always loud: this deployment was deployed with plugins and something is wrong with them.
 */

const INDEX_KEY = 'index.json'

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

  store.setStatus('loading')
  const bucket = getEnterprisePluginsBucket()
  try {
    const index = await fetchIndex(bucket)
    await Promise.all(index.features.map((entry) => loadFeature(bucket, entry, index.releaseVersion)))
    if (isCurrentBucket(bucket)) {
      store.setStatus('ready')
    }
  } catch (error) {
    if (isCurrentBucket(bucket)) {
      console.error('[enterprise] loading failed:', error)
      store.setStatus('error')
    }
  }
}

/** False once a config refresh has replaced or removed the bucket this load started from; its results must then be dropped, not shown. */
function isCurrentBucket(bucket: string): boolean {
  return getEnterprisePluginsBucket() === bucket
}

function recordFeature(bucket: string, feature: EnterpriseFeature): void {
  if (isCurrentBucket(bucket)) {
    useEnterpriseStore.getState().upsertFeature(feature)
  }
}

async function fetchIndex(bucket: string): Promise<PluginIndex> {
  const bytes = await getObjectBytes(bucket, INDEX_KEY)
  return pluginIndexSchema.parse(JSON.parse(new TextDecoder().decode(bytes)))
}

/** Why a feature must not run on this deployment, or null when nothing rules it out. */
function precheckFeature(entry: ManifestFeature, releaseVersion: string | undefined): FeatureStatus | null {
  if (!sdkRangeSatisfied(entry.sdk, HOST_SDK_VERSION)) {
    return 'incompatible'
  }
  const missingModule = (entry.sharedModules ?? []).find((key) => !SHARED_MODULE_KEYS.includes(key))
  if (missingModule) {
    console.error(`[enterprise] "${entry.id}" needs shared module "${missingModule}" this host does not provide`)
    return 'incompatible'
  }
  const requiredStacks = entry.requiresBackend ?? []
  const deployedStacks = getDeployedStacks()
  if (requiredStacks.some((stack) => !deployedStacks.includes(stack))) {
    return 'not-deployed'
  }
  const mismatch = findReleaseMismatch(requiredStacks, getStackReleaseVersions(), releaseVersion)
  if (mismatch) {
    console.error(`[enterprise] "${entry.id}" is from release ${releaseVersion} but its backend stack "${mismatch.stack}" is on ${mismatch.stackVersion}`)
    return 'version-mismatch'
  }
  return null
}

/** A slot this dashboard lacks (an older or modified dashboard) renders nothing, so say so instead of failing silently. */
function reportUnknownSlots(entry: ManifestFeature): void {
  for (const extension of entry.contributes.extensions ?? []) {
    if (!isKnownExtensionSlot(extension.slotId)) {
      console.error(`[enterprise] "${entry.id}" places "${extension.componentId}" in slot "${extension.slotId}", which this dashboard does not have; that part of the feature will not appear`)
    }
  }
}

/** A failure here costs only this feature; the others still load. */
async function loadFeature(
  bucket: string,
  entry: ManifestFeature,
  releaseVersion: string | undefined,
): Promise<void> {
  const blocked = precheckFeature(entry, releaseVersion)
  if (blocked) {
    recordFeature(bucket, { entry, status: blocked, components: {} })
    return
  }
  try {
    const bytes = await getObjectBytes(bucket, entry.bundle)
    if ((await sha384Integrity(bytes)) !== entry.integrity) {
      throw new Error('bundle hash mismatch, refusing to run it')
    }
    const plugin = await importBundle(bytes)
    if (!plugin) {
      throw new Error('bundle never called __RMNG_ENTERPRISE_REGISTER__')
    }
    registerPluginI18n(entry.id, plugin)
    reportUnknownSlots(entry)
    recordFeature(bucket, { entry, status: 'loaded', components: plugin.components })
  } catch (error) {
    console.error(`[enterprise] loading "${entry.id}" failed:`, error)
    recordFeature(bucket, { entry, status: 'failed', components: {} })
  }
}

/**
 * DEV-only escape hatch (`?enterpriseDevUrl=`): treat the URL as a single feature and take its contributions from the plugin's own registration. The hash check is skipped; this path does not exist in production builds.
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

// Bundles register through one global, so imports are serialized: a concurrent import would overwrite the callback, and its `finally` would clear it while the other module is still evaluating.
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

async function getObjectBytes(bucket: string, key: string): Promise<Uint8Array> {
  const response = await getS3Client().send(new GetObjectCommand({ Bucket: bucket, Key: key }))
  if (!response.Body) {
    throw new Error(`empty S3 body for ${key}`)
  }
  return response.Body.transformToByteArray()
}
