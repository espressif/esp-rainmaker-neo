/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useCallback, useEffect, useState } from "react";
import {
  CreateSMSSandboxPhoneNumberCommand,
  DeleteSMSSandboxPhoneNumberCommand,
  GetSMSSandboxAccountStatusCommand,
  ListSMSSandboxPhoneNumbersCommand,
  VerifySMSSandboxPhoneNumberCommand,
  type SMSSandboxPhoneNumber,
} from "@aws-sdk/client-sns";
import { useAwsClients } from "@/hooks/use-aws-client";
import type { SmsSandboxStatus } from "@/config/sms-sandbox-status.config";

/**
 * Small enough that the 10-number sandbox cap always spans more than one page, so the paging
 * controls are exercised rather than decorative.
 */
export const SANDBOX_PAGE_SIZE = 5;

/**
 * The account's sandbox state plus the one fetch state the card needs. The three real
 * statuses live in `sms-sandbox-status.config.ts`, which is what the badge renders from.
 */
export type SandboxAccountStatus = SmsSandboxStatus | "loading";

export function useSmsSandbox() {
  const { clients, error: credsError } = useAwsClients("espuser");
  const sns = clients?.sns ?? null;

  const [accountStatus, setAccountStatus] = useState<SandboxAccountStatus>("loading");

  /**
   * ListSMSSandboxPhoneNumbers pages by opaque token, so there is no page index to jump to: the
   * only way back is to remember the token each visited page was fetched with. The last entry is
   * the current page (the first page's token is `undefined`), Next pushes, Previous pops.
   */
  const [visitedTokens, setVisitedTokens] = useState<(string | undefined)[]>([undefined]);
  const currentToken = visitedTokens[visitedTokens.length - 1];

  const [numbers, setNumbers] = useState<SMSSandboxPhoneNumber[]>([]);
  const [nextToken, setNextToken] = useState<string | undefined>(undefined);
  const [isListLoading, setIsListLoading] = useState(false);
  const [listErrorKey, setListErrorKey] = useState<string | null>(null);
  // Bumped to re-run the list effect when the token has not changed (after a write, or a retry).
  const [reloadCount, setReloadCount] = useState(0);

  useEffect(() => {
    if (!sns) {return;}
    let cancelled = false;
    setAccountStatus("loading");
    sns
      .send(new GetSMSSandboxAccountStatusCommand({}))
      .then((result) => {
        if (!cancelled) {setAccountStatus(result.IsInSandbox ? "sandbox" : "production");}
      })
      .catch(() => {
        if (!cancelled) {setAccountStatus("unknown");}
      });
    return () => {
      cancelled = true;
    };
  }, [sns]);

  useEffect(() => {
    if (!sns || accountStatus !== "sandbox") {return;}
    let cancelled = false;
    setIsListLoading(true);
    setListErrorKey(null);
    sns
      .send(
        new ListSMSSandboxPhoneNumbersCommand({
          MaxResults: SANDBOX_PAGE_SIZE,
          NextToken: currentToken,
        }),
      )
      .then((result) => {
        if (cancelled) {return;}
        setNumbers(result.PhoneNumbers ?? []);
        setNextToken(result.NextToken);
      })
      .catch(() => {
        if (cancelled) {return;}
        setNumbers([]);
        setNextToken(undefined);
        setListErrorKey("smsSandbox.listFailed");
      })
      .finally(() => {
        if (!cancelled) {setIsListLoading(false);}
      });
    return () => {
      cancelled = true;
    };
  }, [sns, accountStatus, currentToken, reloadCount]);

  const goToNextPage = useCallback(() => {
    if (!nextToken) {return;}
    setVisitedTokens((tokens) => [...tokens, nextToken]);
  }, [nextToken]);

  const goToPreviousPage = useCallback(() => {
    setVisitedTokens((tokens) => (tokens.length > 1 ? tokens.slice(0, -1) : tokens));
  }, []);

  const reloadCurrentPage = useCallback(() => {
    setReloadCount((count) => count + 1);
  }, []);

  // A new number can land on any page, so adding one returns to the first page to make it findable.
  const reloadFromFirstPage = useCallback(() => {
    setVisitedTokens([undefined]);
    setReloadCount((count) => count + 1);
  }, []);

  const addNumber = useCallback(
    async (phoneNumber: string) => {
      await sns?.send(new CreateSMSSandboxPhoneNumberCommand({ PhoneNumber: phoneNumber }));
    },
    [sns],
  );

  // AWS sends the one-time password from the add call, and repeating it for a number already
  // pending sends a fresh one — up to five times in 24 hours. There is no separate resend API.
  const resendCode = useCallback(
    async (phoneNumber: string) => {
      await sns?.send(new CreateSMSSandboxPhoneNumberCommand({ PhoneNumber: phoneNumber }));
    },
    [sns],
  );

  const verifyNumber = useCallback(
    async (phoneNumber: string, oneTimePassword: string) => {
      await sns?.send(
        new VerifySMSSandboxPhoneNumberCommand({
          PhoneNumber: phoneNumber,
          OneTimePassword: oneTimePassword,
        }),
      );
    },
    [sns],
  );

  const deleteNumber = useCallback(
    async (phoneNumber: string) => {
      await sns?.send(new DeleteSMSSandboxPhoneNumberCommand({ PhoneNumber: phoneNumber }));
    },
    [sns],
  );

  return {
    credsError,
    accountStatus,
    numbers,
    isListLoading,
    listErrorKey,
    hasPreviousPage: visitedTokens.length > 1,
    hasNextPage: !!nextToken,
    goToNextPage,
    goToPreviousPage,
    reloadCurrentPage,
    reloadFromFirstPage,
    addNumber,
    resendCode,
    verifyNumber,
    deleteNumber,
  };
}
