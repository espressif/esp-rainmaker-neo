/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { getApiGatewayUrl } from '@/lib/config'
import { sigv4Request } from '@/api/sigv4-client'
import type {
  AlexaConfigGetResponse, AlexaConfigRequest, AlexaConfigResponse,
  GvaConfigGetResponse, GvaConfigRequest, GvaConfigResponse, GvaConfigDeleteResponse,
  SmartThingsConfigGetResponse, SmartThingsConfigRequest, SmartThingsConfigResponse,
  SmartThingsConfigDeleteResponse,
  PushIntegrationType, PushIntegrationRequest, ListIntegrationsResponse,
  RegisterIntegrationResponse, IntegrationStatusResponse,
} from './integrations.types'

const ENDPOINTS = {
  alexaConfig: '/v1/admin/integrations/alexa/configuration',
  gvaConfig: '/v1/admin/integrations/gva/configuration',
  smartthingsConfig: '/v1/admin/integrations/smartthings/configuration',
} as const

/**
 * Integrations API functions
 * Uses SigV4-signed requests (IAM auth) to the API Gateway.
 */
export const integrationsApi = {
  getAlexaConfig: async (): Promise<AlexaConfigGetResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.alexaConfig}`
    return sigv4Request<AlexaConfigGetResponse>({ method: 'GET', url })
  },

  configureAlexa: async (data: AlexaConfigRequest): Promise<AlexaConfigResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.alexaConfig}`
    return sigv4Request<AlexaConfigResponse>({ method: 'POST', url, body: data })
  },

  getGvaConfig: async (): Promise<GvaConfigGetResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.gvaConfig}`
    return sigv4Request<GvaConfigGetResponse>({ method: 'GET', url })
  },

  configureGva: async (data: GvaConfigRequest): Promise<GvaConfigResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.gvaConfig}`
    return sigv4Request<GvaConfigResponse>({ method: 'POST', url, body: data })
  },

  deleteGvaConfig: async (): Promise<GvaConfigDeleteResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.gvaConfig}`
    return sigv4Request<GvaConfigDeleteResponse>({ method: 'DELETE', url })
  },

  getSmartThingsConfig: async (): Promise<SmartThingsConfigGetResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.smartthingsConfig}`
    return sigv4Request<SmartThingsConfigGetResponse>({ method: 'GET', url })
  },

  configureSmartThings: async (data: SmartThingsConfigRequest): Promise<SmartThingsConfigResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.smartthingsConfig}`
    return sigv4Request<SmartThingsConfigResponse>({ method: 'POST', url, body: data })
  },

  deleteSmartThingsConfig: async (): Promise<SmartThingsConfigDeleteResponse> => {
    const baseUrl = getApiGatewayUrl()
    if (!baseUrl) {
      throw new Error('API Gateway URL is not configured')
    }

    const url = `${baseUrl.replace(/\/$/, '')}${ENDPOINTS.smartthingsConfig}`
    return sigv4Request<SmartThingsConfigDeleteResponse>({ method: 'DELETE', url })
  },
} as const

function buildUrl(path: string): string {
  const baseUrl = getApiGatewayUrl()
  if (!baseUrl) {
    throw new Error('API Gateway URL is not configured')
  }
  return `${baseUrl.replace(/\/$/, '')}${path}`
}

const INTEGRATIONS_PATH = '/v1/admin/integrations'

/**
 * Push integrations API functions (APNS / APNS_SANDBOX / GCM).
 * Uses SigV4-signed requests (IAM auth) to the API Gateway.
 */
export const pushIntegrationsApi = {
  list: async (): Promise<ListIntegrationsResponse> => {
    return sigv4Request<ListIntegrationsResponse>({ method: 'GET', url: buildUrl(INTEGRATIONS_PATH) })
  },

  register: async (integrationType: PushIntegrationType, data: PushIntegrationRequest): Promise<RegisterIntegrationResponse> => {
    return sigv4Request<RegisterIntegrationResponse>({
      method: 'POST',
      url: buildUrl(`${INTEGRATIONS_PATH}?integration_type=${integrationType}`),
      body: data,
    })
  },

  update: async (integrationId: string, data: PushIntegrationRequest): Promise<IntegrationStatusResponse> => {
    return sigv4Request<IntegrationStatusResponse>({
      method: 'PUT',
      url: buildUrl(`${INTEGRATIONS_PATH}/${encodeURIComponent(integrationId)}`),
      body: data,
    })
  },

  remove: async (integrationId: string): Promise<IntegrationStatusResponse> => {
    return sigv4Request<IntegrationStatusResponse>({
      method: 'DELETE',
      url: buildUrl(`${INTEGRATIONS_PATH}/${encodeURIComponent(integrationId)}`),
    })
  },
} as const
