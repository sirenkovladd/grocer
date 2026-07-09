import van from "vanjs-core"

// Zoomable image with mouse-wheel zoom, click-to-zoom, pinch-to-zoom
// (touch), drag-to-pan, and double-click to reset. Used on pages
// where the user needs to inspect the original photo at full detail
// (e.g. to read a price or item name on a receipt).
//
// Display: the <img> is sized to fit its container via the
// .zoom-image CSS (max-width 100%, max-height 540px, object-fit
// contain). The image element's intrinsic resolution is preserved,
// so zooming in reveals the original full-resolution detail even
// when the image is displayed at a smaller size.
//
// Controls:
//   - Scroll wheel up/down: zoom in/out (1.15x / 0.87x), anchored
//     to the cursor
//   - Click: zoom in (1.5x) toward the click point
//   - Shift+click: zoom out (0.67x) toward the click point
//   - Two-finger pinch: zoom in/out
//   - Drag (mouse or single-finger when zoomed): pan
//   - Double-click: reset to 1x, centered
//
// The component is a one-shot DOM element (not VanJS-reactive) —
// the source URL is bound at construction; pass a fresh component
// when the URL changes.

const { div, img } = van.tags

export const ZoomableImage = (src: () => string, alt: string): HTMLElement => {
  const container = div({ class: "zoom-container" })
  const imgEl = img({ src: src(), alt, class: "zoom-image" })

  let scale = 1
  let panX = 0
  let panY = 0
  let lastPanX = 0
  let lastPanY = 0
  let isDragging = false
  let dragStartX = 0
  let dragStartY = 0
  let lastPinchDist = 0
  let didDrag = false

  const apply = () => {
    imgEl.style.transform = `translate(${panX}px, ${panY}px) scale(${scale})`
    container.style.cursor = scale > 1
      ? (isDragging ? "grabbing" : "grab")
      : "zoom-in"
  }

  // Zoom around a given point in the image (the cursor or
  // finger midpoint). The math keeps the point under the cursor
  // stationary as the scale changes — standard 'zoom to cursor'
  // behavior.
  //
  // IMPORTANT: the container is a flexbox that centers the image
  // (align-items: center; justify-content: center;), so the
  // image element's top-left is not at (0, 0) in container
  // coords — it has a CSS layout offset that depends on the
  // image's aspect ratio and the container's size. The math
  // needs the cursor position in the image's own coordinate
  // frame, not the container's, otherwise the zoom anchor drifts
  // toward the upper-left of the container (where the image
  // starts) on every wheel event. We derive the image's layout
  // offset by reading its current bounding rect and subtracting
  // the current panX/panY (which is the only transform we apply).
  const zoomAt = (x: number, y: number, factor: number) => {
    const oldScale = scale
    scale = Math.min(Math.max(1, scale * factor), 6)
    if (scale === oldScale) return

    if (scale > 1) {
      // Image's CSS layout offset in the container, computed
      // from its current bounding rect (which includes the
      // current transform). transform-origin is 0 0, so scale
      // doesn't move the top-left edge, and the rect's left
      // edge equals cssLayoutX + panX.
      const imgRect = imgEl.getBoundingClientRect()
      const containerRect = container.getBoundingClientRect()
      const cssLayoutX = imgRect.left - containerRect.left - panX
      const cssLayoutY = imgRect.top - containerRect.top - panY

      panX = x - cssLayoutX - (x - cssLayoutX - panX) * factor
      panY = y - cssLayoutY - (y - cssLayoutY - panY) * factor
    } else {
      panX = 0
      panY = 0
    }
    apply()
  }

  // Mouse wheel: zoom in/out, anchored to the cursor.
  container.addEventListener("wheel", (e: WheelEvent) => {
    e.preventDefault()
    const rect = container.getBoundingClientRect()
    const factor = e.deltaY < 0 ? 1.15 : 0.87
    zoomAt(e.clientX - rect.left, e.clientY - rect.top, factor)
  }, { passive: false })

  // Touch: pinch-to-zoom with two fingers, drag-to-pan with one.
  container.addEventListener("touchstart", (e: TouchEvent) => {
    if (e.touches.length === 2) {
      e.preventDefault()
      lastPinchDist = Math.hypot(
        e.touches[0].clientX - e.touches[1].clientX,
        e.touches[0].clientY - e.touches[1].clientY,
      )
    } else if (e.touches.length === 1 && scale > 1) {
      isDragging = true
      didDrag = false
      lastPanX = e.touches[0].clientX - panX
      lastPanY = e.touches[0].clientY - panY
    }
  }, { passive: false })

  container.addEventListener("touchmove", (e: TouchEvent) => {
    if (e.touches.length === 2) {
      e.preventDefault()
      const dist = Math.hypot(
        e.touches[0].clientX - e.touches[1].clientX,
        e.touches[0].clientY - e.touches[1].clientY,
      )
      if (lastPinchDist > 0) {
        const rect = container.getBoundingClientRect()
        const midX = (e.touches[0].clientX + e.touches[1].clientX) / 2 - rect.left
        const midY = (e.touches[0].clientY + e.touches[1].clientY) / 2 - rect.top
        zoomAt(midX, midY, dist / lastPinchDist)
      }
      lastPinchDist = dist
    } else if (e.touches.length === 1 && isDragging) {
      e.preventDefault()
      const dx = e.touches[0].clientX - lastPanX
      const dy = e.touches[0].clientY - lastPanY
      if (Math.abs(dx - panX) > 2 || Math.abs(dy - panY) > 2) didDrag = true
      panX = dx
      panY = dy
      apply()
    }
  }, { passive: false })

  container.addEventListener("touchend", () => {
    isDragging = false
    didDrag = false
    apply()
  })

  // Mouse: drag to pan (when zoomed). Single click resets if
  // currently zoomed (handy escape hatch from zoomed-in state).
  container.addEventListener("mousedown", (e: MouseEvent) => {
    dragStartX = e.clientX
    dragStartY = e.clientY
    if (scale > 1) {
      isDragging = true
      didDrag = false
      lastPanX = e.clientX - panX
      lastPanY = e.clientY - panY
      apply()
    }
  })
  container.addEventListener("mousemove", (e: MouseEvent) => {
    if (isDragging) {
      const dx = e.clientX - lastPanX
      const dy = e.clientY - lastPanY
      if (Math.abs(e.clientX - dragStartX) > 3 || Math.abs(e.clientY - dragStartY) > 3) {
        didDrag = true
      }
      panX = dx
      panY = dy
      apply()
    }
  })
  const stopDrag = () => {
    isDragging = false
    apply()
  }
  container.addEventListener("mouseup", stopDrag)
  container.addEventListener("mouseleave", stopDrag)

  // Click without drag: zoom in (1.5x) anchored to the click
  // position. Shift+click zooms out (0.67x) anchored to the
  // click position. The factors are inverse so a click followed
  // by a shift+click at the same point returns to the original
  // zoom. Shift+click at scale=1 is a no-op (clamped to 1); use
  // double-click to reset.
  container.addEventListener("click", (e: MouseEvent) => {
    if (didDrag) return  // it was a pan, not a click
    const rect = container.getBoundingClientRect()
    const factor = e.shiftKey ? 0.67 : 1.5
    zoomAt(e.clientX - rect.left, e.clientY - rect.top, factor)
  })

  // Double-click: also reset (matches the original implementation
  // and is the convention on touchpads).
  container.addEventListener("dblclick", () => {
    scale = 1
    panX = 0
    panY = 0
    apply()
  })

  van.add(container, imgEl)
  apply()
  return container
}
