/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import {
  getFirmwareFileTags,
  listFirmwareFiles,
  uploadFirmware,
  type FirmwareUploadParams,
  type FirmwareUploadResult,
} from "@/aws/services/firmware-upload.service";

/** Object tags rarely change once an image is uploaded, so they can be cached for a while. */
const TAGS_STALE_TIME = 5 * 60 * 1000;
/** The listing itself is cheap but changes on upload/delete — keep it short-lived. */
const LIST_STALE_TIME = 30 * 1000;

export interface OtaImagesListParams {
  maxKeys: number;
  continuationToken?: string;
  /** Case-sensitive "starts with" filter on the image name (S3 supports prefixes only). */
  namePrefix?: string;
}

export const otaImagesKeys = {
  // Shared prefix for every OTA image query, so a successful upload can invalidate
  // both the listing and the per-object tag reads in one call.
  all: ["s3", "firmware-files"] as const,
  list: (params: OtaImagesListParams) =>
    [...otaImagesKeys.all, "list", params] as const,
  tags: (key: string) => [...otaImagesKeys.all, "tags", key] as const,
};

export const otaImagesQueries = {
  // One page of the OTA bucket listing. `namePrefix` is part of the key so changing the
  // search term refetches; `keepPreviousData` keeps the current page on screen meanwhile.
  firmwareFilesList: (params: OtaImagesListParams) =>
    queryOptions({
      queryKey: otaImagesKeys.list(params),
      queryFn: () =>
        listFirmwareFiles({
          maxKeys: params.maxKeys,
          continuationToken: params.continuationToken,
          namePrefix: params.namePrefix,
          includeTags: false,
        }),
      placeholderData: keepPreviousData,
      staleTime: LIST_STALE_TIME,
    }),

  // Metadata tags (version/type/model/platform) for a single firmware object,
  // read on demand from the S3 object's tag set.
  firmwareTags: (key: string) =>
    queryOptions({
      queryKey: otaImagesKeys.tags(key),
      queryFn: () => getFirmwareFileTags(key),
      staleTime: TAGS_STALE_TIME,
    }),
};

export function useUploadOtaImageMutation() {
  const queryClient = useQueryClient();
  return useMutation<FirmwareUploadResult, Error, FirmwareUploadParams>({
    mutationFn: (params) => uploadFirmware(params),
    onSuccess: () => {
      return queryClient.invalidateQueries({ queryKey: otaImagesKeys.all });
    },
  });
}
