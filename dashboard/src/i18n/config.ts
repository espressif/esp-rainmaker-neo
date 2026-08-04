/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import { appConfig } from '@/lib/app-config'
import type { SupportedLanguage } from '@/lib/constants'
import accountSettingsEn from './locales/en/account-settings.json'
import commonEn from './locales/en/common.json'
import forgotPasswordEn from './locales/en/forgot-password.json'
import generateEn from './locales/en/generate.json'
import loginEn from './locales/en/login.json'
import nodeGroupsEn from './locales/en/node-groups.json'
import nodesEn from './locales/en/nodes.json'
import oauthPreviewEn from './locales/en/oauth-preview.json'
import otaImagesEn from './locales/en/ota-images.json'
import otaJobsEn from './locales/en/ota-jobs.json'
import postDeploymentEn from './locales/en/post-deployment.json'
import pushNotificationsEn from './locales/en/push-notifications.json'
import registerEn from './locales/en/register.json'
import setPasswordEn from './locales/en/set-password.json'
import staticEn from './locales/en/static.json'
import voiceAssistantsEn from './locales/en/voice-assistants.json'
import accountSettingsZh from './locales/zh/account-settings.json'
import commonZh from './locales/zh/common.json'
import forgotPasswordZh from './locales/zh/forgot-password.json'
import generateZh from './locales/zh/generate.json'
import loginZh from './locales/zh/login.json'
import nodeGroupsZh from './locales/zh/node-groups.json'
import nodesZh from './locales/zh/nodes.json'
import oauthPreviewZh from './locales/zh/oauth-preview.json'
import otaImagesZh from './locales/zh/ota-images.json'
import otaJobsZh from './locales/zh/ota-jobs.json'
import postDeploymentZh from './locales/zh/post-deployment.json'
import pushNotificationsZh from './locales/zh/push-notifications.json'
import registerZh from './locales/zh/register.json'
import setPasswordZh from './locales/zh/set-password.json'
import staticZh from './locales/zh/static.json'
import voiceAssistantsZh from './locales/zh/voice-assistants.json'

/**
 * One namespace per sidebar page or primary route; `common` holds only what is
 * shared across routes. See `.cursor/rules/admin-dashboard.mdc` for the ownership
 * rules and `npm run check:i18n` for the gate that enforces them.
 */
const resources = {
  en: {
    common: commonEn,
    login: loginEn,
    'forgot-password': forgotPasswordEn,
    'set-password': setPasswordEn,
    'oauth-preview': oauthPreviewEn,
    static: staticEn,
    nodes: nodesEn,
    'node-groups': nodeGroupsEn,
    register: registerEn,
    generate: generateEn,
    'ota-images': otaImagesEn,
    'ota-jobs': otaJobsEn,
    'voice-assistants': voiceAssistantsEn,
    'push-notifications': pushNotificationsEn,
    'post-deployment': postDeploymentEn,
    'account-settings': accountSettingsEn,
  },
  zh: {
    common: commonZh,
    login: loginZh,
    'forgot-password': forgotPasswordZh,
    'set-password': setPasswordZh,
    'oauth-preview': oauthPreviewZh,
    static: staticZh,
    nodes: nodesZh,
    'node-groups': nodeGroupsZh,
    register: registerZh,
    generate: generateZh,
    'ota-images': otaImagesZh,
    'ota-jobs': otaJobsZh,
    'voice-assistants': voiceAssistantsZh,
    'push-notifications': pushNotificationsZh,
    'post-deployment': postDeploymentZh,
    'account-settings': accountSettingsZh,
  },
} satisfies Record<SupportedLanguage, object>

void i18n
  .use(initReactI18next)
  .init({
    resources,
    lng: appConfig.defaults.language,
    fallbackLng: appConfig.defaults.language,
    defaultNS: 'common',
    interpolation: {
      escapeValue: false,
    },
  })

// Keep <html lang> in step with the active language. The build-time value comes
// from appConfig.defaults.language (vite-plugins/app-head.ts); this covers the
// ?hl= param and the in-app language picker, both of which land here via the
// store -> i18next sync in app/routes/__root.tsx.
i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
})

export default i18n
