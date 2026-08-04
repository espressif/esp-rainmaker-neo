/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

/**
 * Verhoeff check-digit algorithm.
 *
 * Matter's manual pairing code appends a Verhoeff check digit over the decimal
 * digit string (esp-matter-mfg-tool uses python-stdnum `stdnum.verhoeff`).
 */

// Multiplication table (dihedral group D5).
const D = [
  [0, 1, 2, 3, 4, 5, 6, 7, 8, 9],
  [1, 2, 3, 4, 0, 6, 7, 8, 9, 5],
  [2, 3, 4, 0, 1, 7, 8, 9, 5, 6],
  [3, 4, 0, 1, 2, 8, 9, 5, 6, 7],
  [4, 0, 1, 2, 3, 9, 5, 6, 7, 8],
  [5, 9, 8, 7, 6, 0, 4, 3, 2, 1],
  [6, 5, 9, 8, 7, 1, 0, 4, 3, 2],
  [7, 6, 5, 9, 8, 2, 1, 0, 4, 3],
  [8, 7, 6, 5, 9, 3, 2, 1, 0, 4],
  [9, 8, 7, 6, 5, 4, 3, 2, 1, 0],
]

// Permutation table.
const P = [
  [0, 1, 2, 3, 4, 5, 6, 7, 8, 9],
  [1, 5, 7, 6, 2, 8, 3, 0, 9, 4],
  [5, 8, 0, 3, 7, 9, 6, 1, 4, 2],
  [8, 9, 1, 6, 0, 4, 3, 5, 2, 7],
  [9, 4, 5, 3, 1, 2, 6, 8, 7, 0],
  [4, 2, 8, 6, 5, 7, 3, 9, 0, 1],
  [2, 7, 9, 3, 8, 0, 6, 4, 1, 5],
  [7, 0, 4, 6, 9, 1, 3, 2, 5, 8],
]

// Inverse table.
const INV = [0, 4, 3, 2, 1, 5, 6, 7, 8, 9]

/** Compute the Verhoeff check digit for a string of decimal digits. */
export function verhoeffCheckDigit(numStr: string): string {
  let c = 0
  const reversed = numStr.split('').reverse()
  for (let i = 0; i < reversed.length; i++) {
    const digit = reversed[i].charCodeAt(0) - 48
    if (digit < 0 || digit > 9) {throw new Error(`non-digit in input: ${reversed[i]}`)}
    c = D[c][P[(i + 1) % 8][digit]]
  }
  return String(INV[c])
}
