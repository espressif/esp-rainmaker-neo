/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type {
  LambdaConcurrency,
  SesAccessState,
  SesReviewOutcome,
  SnsMonthlySpend,
} from "@/aws/services/deployment-limits.service";
import type { SesAccessStatus } from "@/config/ses-access-status.config";
import { COGNITO_DAILY_EMAIL_CAP } from "./post-deployment.constants";
import type { ValueReading } from "./values.types";

/**
 * Maps the AWS-layer domain results onto what the page displays. Kept here rather
 * than in `src/aws/services/` so the service functions stay free of i18n keys and
 * of this page's translation namespace.
 */

const REVIEW_STATUSES: Record<SesReviewOutcome, SesAccessStatus> = {
  pending: "reviewPending",
  denied: "reviewDenied",
  failed: "reviewFailed",
};

export function toSesAccessStatus(state: SesAccessState): SesAccessStatus {
  if (state.kind === "production") {
    return "production";
  }
  return state.review ? REVIEW_STATUSES[state.review] : "sandbox";
}

export function sesAccessReading(state: SesAccessState): ValueReading {
  return { display: "ses-access", status: toSesAccessStatus(state) };
}

export function cognitoEmailCapReading(): ValueReading {
  return {
    display: "text",
    i18nKey: "values.cognito_email_cap.readings.cap",
    // Not `count`: that is i18next's pluralization variable and would route this
    // through plural resolution for no reason.
    defaultValue: "{{cap, number}} emails / day per AWS account",
    vars: { cap: COGNITO_DAILY_EMAIL_CAP },
  };
}

export function snsMonthlySpendReading({
  limitUsd,
}: SnsMonthlySpend): ValueReading {
  if (limitUsd == null) {
    return {
      display: "text",
      i18nKey: "values.sns_monthly_spend.readings.limitDefault",
      defaultValue: "${{limit, number}} / month (default)",
      // AWS omits the attribute until it has been set, and the account default is $1.
      vars: { limit: 1 },
    };
  }
  return {
    display: "text",
    i18nKey: "values.sns_monthly_spend.readings.limit",
    defaultValue: "${{limit, number}} / month",
    vars: { limit: limitUsd },
  };
}

export function lambdaConcurrencyReading({
  concurrentExecutions,
}: LambdaConcurrency): ValueReading {
  if (concurrentExecutions == null) {
    return {
      display: "text",
      i18nKey: "values.lambda_concurrency.readings.unknown",
      defaultValue: "Unknown",
    };
  }
  return {
    display: "text",
    i18nKey: "values.lambda_concurrency.readings.concurrency",
    defaultValue: "{{limit, number}} concurrent executions",
    vars: { limit: concurrentExecutions },
  };
}
