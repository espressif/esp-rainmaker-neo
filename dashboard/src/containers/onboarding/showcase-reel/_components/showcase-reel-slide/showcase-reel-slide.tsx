/*
 * SPDX-FileCopyrightText: 2026 Espressif Systems (Shanghai) CO LTD
 *
 * SPDX-License-Identifier: Apache-2.0
 */

import { useEffect, useRef, useState } from "react";

/** How long a slide whose video failed to load shows its poster before the carousel moves on. */
const FAILED_SLIDE_MS = 4000;
import type { ShowcaseReelSlideProps } from "./showcase-reel-slide.props";

export default function ShowcaseReelSlide({
  sources,
  label,
  isActive,
  shouldPreload,
  isPaused,
  onEnded,
}: ShowcaseReelSlideProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [hasFailed, setHasFailed] = useState(false);

  // `ended` never fires for a video that failed to load, so without this the carousel would stall on that slide.
  useEffect(() => {
    if (!isActive || !hasFailed || isPaused) {return;}
    const timer = window.setTimeout(onEnded, FAILED_SLIDE_MS);
    return () => window.clearTimeout(timer);
  }, [isActive, hasFailed, isPaused, onEnded]);

  // Each clip's last frame equals its first, so a slide coming into view always restarts from 0.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || isActive) {return;}
    video.pause();
    video.currentTime = 0;
  }, [isActive]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !isActive) {return;}
    if (isPaused) {
      video.pause();
      return;
    }
    video.play().catch((error: unknown) => {
      console.warn("Showcase reel autoplay was blocked", error);
    });
  }, [isActive, isPaused]);

  return (
    <video
      ref={videoRef}
      className="h-screen w-full object-cover"
      poster={sources.poster}
      muted
      playsInline
      preload={shouldPreload ? "auto" : "metadata"}
      aria-label={label}
      onEnded={onEnded}
    >
      <source src={sources.mp4} type="video/mp4" onError={() => setHasFailed(true)} />
    </video>
  );
}
