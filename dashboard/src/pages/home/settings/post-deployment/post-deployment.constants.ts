/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** The AWS service a value belongs to, which is how the page groups them. */
export const POST_DEPLOYMENT_SECTIONS = {
  ses: "ses",
  sns: "sns",
  lambda: "lambda",
} as const;

export type PostDeploymentSection =
  (typeof POST_DEPLOYMENT_SECTIONS)[keyof typeof POST_DEPLOYMENT_SECTIONS];

/** Tab order, which also drives the scroll-spy order. */
export const POST_DEPLOYMENT_SECTION_IDS: readonly PostDeploymentSection[] = [
  POST_DEPLOYMENT_SECTIONS.ses,
  POST_DEPLOYMENT_SECTIONS.sns,
  POST_DEPLOYMENT_SECTIONS.lambda,
];

/**
 * Cognito's built-in email sender is capped at 50 messages per day for the whole
 * AWS account. Fixed by AWS and not raisable, so it is stated rather than read.
 */
export const COGNITO_DAILY_EMAIL_CAP = 50;
