/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Integrations API module exports
 */

export type {
  AlexaConfigGetResponse,
  AlexaConfigRequest,
  AlexaConfigResponse,
  GvaServiceAccount,
  GvaConfigGetResponse,
  GvaConfigRequest,
  GvaConfigResponse,
  GvaConfigDeleteResponse,
  SmartThingsConfigGetResponse,
  SmartThingsConfigRequest,
  SmartThingsConfigResponse,
  SmartThingsConfigDeleteResponse,
  PushIntegrationType,
  PushIntegrationRequest,
  IntegrationDetail,
  ListIntegrationsResponse,
  RegisterIntegrationResponse,
  IntegrationStatusResponse,
} from './integrations.types'

export { PUSH_INTEGRATION_TYPES } from './integrations.types'

export { integrationsApi, pushIntegrationsApi } from './integrations.api'

export {
  integrationsKeys,
  integrationsQueries,
  useGetAlexaConfig,
  useConfigureAlexa,
  gvaQueries,
  useGetGvaConfig,
  useConfigureGva,
  useDeleteGvaConfig,
  smartthingsQueries,
  useGetSmartThingsConfig,
  useConfigureSmartThings,
  useDeleteSmartThingsConfig,
  pushIntegrationsKeys,
  usePushIntegrations,
  useRegisterPushIntegration,
  useUpdatePushIntegration,
  useDeletePushIntegration,
} from './integrations.queries'
