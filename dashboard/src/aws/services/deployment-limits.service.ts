/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { GetAccountCommand } from "@aws-sdk/client-sesv2";
import { GetSMSAttributesCommand } from "@aws-sdk/client-sns";
import { GetAccountSettingsCommand } from "@aws-sdk/client-lambda";
import type { ScopedAwsCredentials } from "@/api/post-deployment/post-deployment.types";
import {
  getScopedLambdaClient,
  getScopedSesClient,
  getScopedSnsClient,
} from "./scoped-clients";

export interface ReadLimitOptions {
  signal?: AbortSignal;
}

/**
 * Outcome of an AWS review of a production-access request. A closed review is
 * not a sandbox to ask out of again, so it reads differently from a plain
 * sandbox account.
 */
export type SesReviewOutcome = "pending" | "denied" | "failed";

export type SesAccessState =
  | { kind: "production" }
  | { kind: "sandbox"; review?: SesReviewOutcome };

export interface SnsMonthlySpend {
  /** Null when AWS has not set the attribute, which means the $1 account default. */
  limitUsd: number | null;
}

export interface LambdaConcurrency {
  concurrentExecutions: number | null;
}

function toReviewOutcome(status: string | undefined): SesReviewOutcome | undefined {
  if (status === "PENDING") {
    return "pending";
  }
  if (status === "DENIED") {
    return "denied";
  }
  if (status === "FAILED") {
    return "failed";
  }
  return undefined;
}

export async function readSesAccessState(
  creds: ScopedAwsCredentials,
  { signal }: ReadLimitOptions = {},
): Promise<SesAccessState> {
  const account = await getScopedSesClient(creds).send(
    new GetAccountCommand({}),
    { abortSignal: signal },
  );

  if (account.ProductionAccessEnabled) {
    return { kind: "production" };
  }
  return {
    kind: "sandbox",
    review: toReviewOutcome(account.Details?.ReviewDetails?.Status),
  };
}

export async function readSnsMonthlySpend(
  creds: ScopedAwsCredentials,
  { signal }: ReadLimitOptions = {},
): Promise<SnsMonthlySpend> {
  const attributes = await getScopedSnsClient(creds).send(
    new GetSMSAttributesCommand({}),
    { abortSignal: signal },
  );

  // AWS reports the limit as a string, and omits it entirely until it has been set.
  const rawLimit = attributes.attributes?.MonthlySpendLimit;
  if (rawLimit == null) {
    return { limitUsd: null };
  }
  const limitUsd = Number(rawLimit);
  return { limitUsd: Number.isFinite(limitUsd) ? limitUsd : null };
}

export async function readLambdaConcurrency(
  creds: ScopedAwsCredentials,
  { signal }: ReadLimitOptions = {},
): Promise<LambdaConcurrency> {
  const settings = await getScopedLambdaClient(creds).send(
    new GetAccountSettingsCommand({}),
    { abortSignal: signal },
  );

  return {
    concurrentExecutions: settings.AccountLimit?.ConcurrentExecutions ?? null,
  };
}
