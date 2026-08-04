/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import type { LucideIcon } from "lucide-react";
import { Mail, MessageSquare, Zap } from "lucide-react";
import type { TFunction } from "i18next";
import {
  POST_DEPLOYMENT_SECTIONS,
  POST_DEPLOYMENT_SECTION_IDS,
  type PostDeploymentSection,
} from "../../post-deployment.constants";

export interface PostDeploymentSectionConfig {
  id: PostDeploymentSection;
  Icon: LucideIcon;
  label: string;
  description: string;
}

const SECTION_ICONS: Record<PostDeploymentSection, LucideIcon> = {
  [POST_DEPLOYMENT_SECTIONS.ses]: Mail,
  [POST_DEPLOYMENT_SECTIONS.sns]: MessageSquare,
  [POST_DEPLOYMENT_SECTIONS.lambda]: Zap,
};

const SECTION_FALLBACKS: Record<
  PostDeploymentSection,
  { label: string; description: string }
> = {
  [POST_DEPLOYMENT_SECTIONS.ses]: {
    label: "Email sending (SES)",
    description: "How the platform sends email, and who can receive it today.",
  },
  [POST_DEPLOYMENT_SECTIONS.sns]: {
    label: "SMS sending (SNS)",
    description:
      "How the platform sends SMS, and which destination numbers are reachable.",
  },
  [POST_DEPLOYMENT_SECTIONS.lambda]: {
    label: "Compute (Lambda)",
    description:
      "The account limit on how much traffic the platform can serve at once.",
  },
};

export function buildPostDeploymentSections(
  t: TFunction<"post-deployment">,
): PostDeploymentSectionConfig[] {
  return POST_DEPLOYMENT_SECTION_IDS.map((id) => ({
    id,
    Icon: SECTION_ICONS[id],
    label: t(`sections.${id}.title`, SECTION_FALLBACKS[id].label),
    description: t(
      `sections.${id}.description`,
      SECTION_FALLBACKS[id].description,
    ),
  }));
}
