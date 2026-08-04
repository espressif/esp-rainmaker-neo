/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { ComponentType } from "react"

import {
  AmazonAlexaIcon,
  AndroidIcon,
  Android2Icon,
  AppleIcon,
  AwsCognitoIcon,
  AwsIcon,
  GithubIcon,
  GoogleAssistantIcon,
  GoogleIcon,
  MatterIcon,
  OauthIcon,
  SecurityIcon,
  WechatIcon,
} from "./icons"

export interface CustomIconEntry {
  component: ComponentType<{ size?: number }>
}

/**
 * To add or remove a custom icon:
 * 1. Create a React component in ./icons/ with the SVG inlined, following the existing pattern
 * 2. Export it from ./icons/index.ts
 * 3. Add an entry here with the kebab-case icon id as the key
 */
export const customIconRegistry = {
  "amazon-alexa": { component: AmazonAlexaIcon },
  android: { component: AndroidIcon },
  android2: { component: Android2Icon },
  apple: { component: AppleIcon },
  "aws-cognito": { component: AwsCognitoIcon },
  aws: { component: AwsIcon },
  github: { component: GithubIcon },
  google: { component: GoogleIcon },
  "google-assistant": { component: GoogleAssistantIcon },
  matter: { component: MatterIcon },
  oauth: { component: OauthIcon },
  security: { component: SecurityIcon },
  wechat: { component: WechatIcon },
} as const

export type CustomIconId = keyof typeof customIconRegistry

export const customIconIds = Object.keys(customIconRegistry) as CustomIconId[]
