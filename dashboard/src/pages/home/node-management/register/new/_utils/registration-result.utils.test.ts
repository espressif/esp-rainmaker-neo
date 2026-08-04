/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { describe, expect, it } from "vitest";
import type { TFunction } from "i18next";
import type { RegistrationJobStatusResponse } from "@/api/node-registration";
import { deriveRegistrationResultAlert } from "./registration-result.utils";

/** Echoes the fallback with interpolation applied, so assertions read the real copy. */
const t = ((
  key: string,
  fallback: string,
  vars?: Record<string, string | number>,
) => {
  if (!vars) {
    return fallback;
  }
  return fallback.replace(/{{(\w+)}}/g, (_, name: string) =>
    String(vars[name] ?? ""),
  );
}) as TFunction<"nodes">;

const FAILED_FILE = "s3://bucket/system/node_certs/abc_failed_node_certs.csv";

function job(
  overrides: Partial<RegistrationJobStatusResponse>,
): RegistrationJobStatusResponse {
  return {
    request_id: "req-1",
    status: "completed",
    total_nodes: 5,
    message: "Bulk node registration completed",
    ...overrides,
  };
}

describe("deriveRegistrationResultAlert", () => {
  it("reports every node failing as an error, not a generic completion", () => {
    const result = deriveRegistrationResultAlert(
      job({ success_count: 0, failed_count: 5, failed_file_s3_path: FAILED_FILE }),
      t,
    );

    expect(result.kind).toBe("all-failed");
    expect(result.type).toBe("error");
    expect(result.title).toBe("Failed to register nodes");
    expect(result.description).toBe(
      "None of the 5 nodes could be registered. Download the failed nodes file to see why.",
    );
    expect(result.failedFileS3Path).toBe(FAILED_FILE);
    // The generic backend message must not leak through.
    expect(result.description).not.toContain("Bulk node registration completed");
  });

  it("reports a partial failure as a warning with both counts", () => {
    const result = deriveRegistrationResultAlert(
      job({ success_count: 3, failed_count: 2, failed_file_s3_path: FAILED_FILE }),
      t,
    );

    expect(result.kind).toBe("partial");
    expect(result.type).toBe("warning");
    expect(result.description).toBe(
      "3 of 5 nodes were registered. 2 failed — download the failed nodes file to see why.",
    );
  });

  it("omits the download hint when the job produced no failed-nodes file", () => {
    const result = deriveRegistrationResultAlert(
      job({ success_count: 3, failed_count: 2 }),
      t,
    );

    expect(result.description).toBe("3 of 5 nodes were registered. 2 failed.");
    expect(result.failedFileS3Path).toBeUndefined();
  });

  it("reports full success", () => {
    const result = deriveRegistrationResultAlert(
      job({ success_count: 5, failed_count: 0 }),
      t,
    );

    expect(result.kind).toBe("success");
    expect(result.type).toBe("success");
    expect(result.description).toBe(
      "All 5 nodes were registered and are ready to use.",
    );
    expect(result.failedFileS3Path).toBeUndefined();
  });

  it("does not claim success when the counts are absent but a failed-nodes file exists", () => {
    const result = deriveRegistrationResultAlert(
      job({ failed_file_s3_path: FAILED_FILE }),
      t,
    );

    expect(result.kind).toBe("partial");
    expect(result.type).toBe("warning");
    expect(result.failedFileS3Path).toBe(FAILED_FILE);
  });

  it("never renders a 0-of-0 summary when total_nodes is missing", () => {
    const result = deriveRegistrationResultAlert(
      job({ total_nodes: 0, success_count: 0, failed_count: 0 }),
      t,
    );

    expect(result.kind).toBe("success");
    expect(result.description).toBe(
      "Every node in the certificate file was registered and is ready to use.",
    );
  });

  it("reconstructs the total from the counts when total_nodes lags", () => {
    const result = deriveRegistrationResultAlert(
      job({ total_nodes: 0, success_count: 4, failed_count: 1 }),
      t,
    );

    expect(result.kind).toBe("partial");
    expect(result.description).toBe("4 of 5 nodes were registered. 1 failed.");
  });

  it("surfaces the backend message for a job that errored out", () => {
    const result = deriveRegistrationResultAlert(
      job({ status: "failed", message: "Container exited unexpectedly" }),
      t,
    );

    expect(result.kind).toBe("job-error");
    expect(result.type).toBe("error");
    expect(result.title).toBe("Registration job failed");
    expect(result.description).toBe("Container exited unexpectedly");
  });
});
