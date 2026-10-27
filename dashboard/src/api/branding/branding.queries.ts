/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  deleteBrand,
  publishBrand,
  readPublishedBrand,
} from "@/aws/services/brand.service";
import { getServerUrl } from "@/lib/config";
import type { PublishedBrand } from "@/utils/rainmaker-brand/rainmaker-brand";

const PUBLISHED_BRAND_STALE_MS = 60 * 1000;

export const brandingKeys = {
  all: ["branding"] as const,
  published: (outputsUrl: string) =>
    [...brandingKeys.all, "published", outputsUrl] as const,
};

export const brandingQueries = {
  published: () =>
    queryOptions({
      queryKey: brandingKeys.published(getServerUrl()),
      queryFn: readPublishedBrand,
      staleTime: PUBLISHED_BRAND_STALE_MS,
      retry: 1,
    }),
};

export function usePublishedBrand() {
  return useQuery(brandingQueries.published());
}

export function usePublishBrandMutation() {
  const queryClient = useQueryClient();

  return useMutation<PublishedBrand, Error, File>({
    mutationFn: publishBrand,
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: brandingKeys.all });
    },
  });
}

export function useDeleteBrandMutation() {
  const queryClient = useQueryClient();

  return useMutation<void, Error, void>({
    mutationFn: deleteBrand,
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: brandingKeys.all });
    },
  });
}
