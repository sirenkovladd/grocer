import { onMount } from "solid-js"
import { ImageCropper as makeCropper, type CropperOptions } from "./cropper.ts"

// Solid wrapper around the framework-free cropper.ts. The imperative
// factory returns { getElement } and attaches all listeners to owned
// elements, so mounting is appendChild and disposal is automatic GC
// — no onCleanup needed. All crop logic stays in cropper.ts; this
// file is only the Solid mount point for the upload page.
export default function ImageCropperView(props: CropperOptions) {
  let host!: HTMLDivElement
  onMount(() => {
    const { getElement } = makeCropper(props)
    host.appendChild(getElement())
  })
  return <div ref={host} />
}

export type { CropperOptions }
