/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useSetState } from "react-use";

export interface TokenPaginationState {
  nextToken?: string;
  tokenHistory: (string | undefined)[];
  pageIndex: number;
  pageSize: number;
}

export function resetTokenPagination() {
  return {
    nextToken: undefined as string | undefined,
    tokenHistory: [undefined] as (string | undefined)[],
    pageIndex: 0,
  };
}

export function useTokenPagination(initialPageSize: number) {
  const [state, setState] = useSetState<TokenPaginationState>({
    nextToken: undefined,
    tokenHistory: [undefined],
    pageIndex: 0,
    pageSize: initialPageSize,
  });

  const hasPrevPage = state.pageIndex > 0;

  const handlePageSizeChange = (size: number) => {
    setState({ pageSize: size, ...resetTokenPagination() });
  };

  const goNext = (nextToken: string | undefined) => {
    if (!nextToken) {return;}
    const newIndex = state.pageIndex + 1;
    const newHistory = [...state.tokenHistory];
    if (newHistory.length <= newIndex + 1) {
      newHistory.push(nextToken);
    }
    setState({
      tokenHistory: newHistory,
      nextToken,
      pageIndex: newIndex,
    });
  };

  const goPrev = () => {
    if (state.pageIndex <= 0) {return;}
    const newIndex = state.pageIndex - 1;
    setState({
      nextToken: state.tokenHistory[newIndex],
      pageIndex: newIndex,
    });
  };

  const resetPagination = () => {
    setState(resetTokenPagination());
  };

  return {
    state,
    hasPrevPage,
    handlePageSizeChange,
    goNext,
    goPrev,
    resetPagination,
  };
}
