/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/** ScrollableSections section ids. */
export const SECTION_BASIC_DETAILS = "basic-details";
export const SECTION_SUBGROUP = "subgroup";
export const SECTION_DYNAMIC = "dynamic-group";

/** Switch-card ids for the two mutually-exclusive membership modes. */
export const SUBGROUP_CARD_ID = "subgroup";
export const DYNAMIC_CARD_ID = "dynamic";

/** Name may contain letters, numbers, colons, underscores and hyphens only. */
export const GROUP_NAME_REGEX = /^[a-zA-Z0-9:_-]+$/;

/** Max length accepted for the group name. */
export const GROUP_NAME_MAX_LENGTH = 128;
