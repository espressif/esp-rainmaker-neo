/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export * from './types';
export { SUPPORTED_LANGUAGES, type SupportedLanguage } from '../constants';
export { defineConfig } from './define-config';
export { appStorageKey } from './storage-key';

// Re-export the app config instance for convenient access via @/lib/app-config
export { default as appConfig } from '../../../app.config';
