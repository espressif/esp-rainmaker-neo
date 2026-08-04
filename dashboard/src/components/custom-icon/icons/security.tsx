/*
 * SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

export function SecurityIcon({ size = 18 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden
    >
      <g transform="translate(4 1)">
        <path
          d="M8 0L-1 4V10C-1 15.6 2.8 20.7 8 22C13.2 20.7 17 15.6 17 10V4L8 0ZM8 11H15C14.5 15.1 11.7 18.8 8 19.9V11H1V5.3L8 2.2V11Z"
          fill="var(--color-primary)"
          fillOpacity="0.65"
        />
        <path
          d="M8 0V22C13.2 20.7 17 15.6 17 10V4L8 0ZM15 11C14.5 15.1 11.7 18.8 8 19.9V11H15Z"
          fill="var(--color-primary)"
          fillOpacity="0.9"
        />
        <path
          d="M17 11H15C15 11 15 11.3 14.9 11.6L17 11Z"
          fill="var(--color-primary)"
          fillRule="evenodd"
        />
        <polygon
          points="-1,11 1,11 1,10.4"
          fill="var(--color-primary)"
          fillRule="evenodd"
        />
      </g>
    </svg>
  )
}
