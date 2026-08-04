/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import type {
  ReadLimitOptions,
} from "@/aws/services/deployment-limits.service";
import type {
  ScopedAwsCredentials,
  StepStack,
} from "@/api/post-deployment/post-deployment.types";
import type { SesAccessStatus } from "@/config/ses-access-status.config";
import type { PostDeploymentSection } from "./post-deployment.constants";

/**
 * A limit rendered as monospace text. Carried as a translation key plus its
 * interpolation values rather than a formatted string: the AWS read happens once
 * and is cached, but the rendered text has to follow the active language.
 *
 * Must stay cache-safe — plain strings and numbers only.
 */
export interface TextReading {
  display: "text";
  /** Key under `values.<id>.readings.*` in the `post-deployment` namespace. */
  i18nKey: string;
  /** Fallback for {@link i18nKey}; interpolated and formatted like the real string. */
  defaultValue: string;
  vars?: Record<string, string | number>;
}

/** A limit that is a discrete state, so it renders as a status badge. */
export interface SesAccessReading {
  display: "ses-access";
  status: SesAccessStatus;
}

export type ValueReading = TextReading | SesAccessReading;

/**
 * One account limit the platform runs against. Each is reported, never changed:
 * raising any of them is an AWS-side action an operator takes in the console or
 * through a support case, so the note says why production needs it and how to get
 * it rather than offering a button that would only fail.
 */
interface ValueBase {
  /** Also the reading query's cache key, so it must be unique across {@link VALUES}. */
  id: string;
  section: PostDeploymentSection;
  Icon: LucideIcon;
  titleKey: string;
  /** Why production needs this, and how to raise it. */
  noteKey: string;
}

/** Fixed by AWS, so it is stated rather than looked up and needs no credentials. */
export interface FixedValue extends ValueBase {
  credsFree: true;
  reading: ValueReading;
}

/** Read live from the account, with the stack whose credentials can read it. */
export interface LookedUpValue extends ValueBase {
  credsFree?: false;
  stack: StepStack;
  read: (
    creds: ScopedAwsCredentials,
    options?: ReadLimitOptions,
  ) => Promise<ValueReading>;
}

export type DeploymentValue = FixedValue | LookedUpValue;
