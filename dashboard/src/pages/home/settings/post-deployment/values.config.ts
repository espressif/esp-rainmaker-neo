/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { DollarSign, Gauge, Send, ShieldCheck, Smartphone } from "lucide-react";
import {
  readLambdaConcurrency,
  readSesAccessState,
  readSnsMonthlySpend,
  type ReadLimitOptions,
} from "@/aws/services/deployment-limits.service";
import type { ScopedAwsCredentials } from "@/api/post-deployment/post-deployment.types";
import { POST_DEPLOYMENT_SECTIONS } from "./post-deployment.constants";
import {
  cognitoEmailCapReading,
  lambdaConcurrencyReading,
  sesAccessReading,
  snsMonthlySpendReading,
} from "./values.utils";
import type { DeploymentValue } from "./values.types";

export const VALUES = [
  {
    // Fixed by AWS for the built-in Cognito sender, so there is nothing to look up.
    // Grouped with SES because it is about email delivery, and pointing the pool at
    // SES is how the cap is lifted.
    id: "cognito_email_cap",
    section: POST_DEPLOYMENT_SECTIONS.ses,
    Icon: Send,
    titleKey: "values.cognito_email_cap.title",
    noteKey: "values.cognito_email_cap.note",
    credsFree: true,
    reading: cognitoEmailCapReading(),
  },
  {
    id: "ses_sandbox",
    section: POST_DEPLOYMENT_SECTIONS.ses,
    Icon: ShieldCheck,
    stack: "espuser",
    titleKey: "values.ses_sandbox.title",
    noteKey: "values.ses_sandbox.note",
    read: async (creds: ScopedAwsCredentials, options?: ReadLimitOptions) =>
      sesAccessReading(await readSesAccessState(creds, options)),
  },
  {
    id: "sns_monthly_spend",
    section: POST_DEPLOYMENT_SECTIONS.sns,
    Icon: DollarSign,
    stack: "espuser",
    titleKey: "values.sns_monthly_spend.title",
    noteKey: "values.sns_monthly_spend.note",
    read: async (creds: ScopedAwsCredentials, options?: ReadLimitOptions) =>
      snsMonthlySpendReading(await readSnsMonthlySpend(creds, options)),
  },
  {
    id: "lambda_concurrency",
    section: POST_DEPLOYMENT_SECTIONS.lambda,
    Icon: Gauge,
    stack: "rmng",
    titleKey: "values.lambda_concurrency.title",
    noteKey: "values.lambda_concurrency.note",
    read: async (creds: ScopedAwsCredentials, options?: ReadLimitOptions) =>
      lambdaConcurrencyReading(await readLambdaConcurrency(creds, options)),
  },
] as const satisfies readonly DeploymentValue[];

/** Ids are the cache keys for the reading queries, so a duplicate must not compile. */
export type DeploymentValueId = (typeof VALUES)[number]["id"];

/**
 * The SMS sandbox is the one value an operator can act on from this page —
 * registering and verifying a destination number are ordinary SNS calls the scoped
 * credentials already allow — so it is rendered by its own interactive card rather
 * than as a read-only value.
 */
export const SMS_SANDBOX_CARD = {
  section: POST_DEPLOYMENT_SECTIONS.sns,
  Icon: Smartphone,
  titleKey: "values.sns_sms_sandbox.title",
  noteKey: "values.sns_sms_sandbox.note",
} as const;
