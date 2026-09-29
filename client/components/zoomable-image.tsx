import { onMount } from "solid-js"
import { ZoomableImage as makeZoomable } from "./zoomable-image.ts"

interface ZoomableImageViewProps {
  src: string
  alt?: string
}

// Solid wrapper around the framework-free zoomable-image.ts (wheel /
// click / pinch zoom + drag pan, all listeners on owned elements so
// disposal is automatic GC — no onCleanup needed).
//
// The underlying element binds its source URL at construction; when
// the URL changes, mount a fresh component (e.g. key the parent
// branch on the URL) instead of updating props.
export default function ZoomableImageView(props: ZoomableImageViewProps) {
  let host!: HTMLDivElement
  onMount(() => {
    host.appendChild(makeZoomable(() => props.src, props.alt ?? ""))
  })
  return <div ref={host} />
}
