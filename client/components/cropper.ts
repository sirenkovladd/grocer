import van from "vanjs-core"

interface CropRect {
  x: number
  y: number
  width: number
  height: number
}

interface CropperOptions {
  imageUrl: string
  imageHash: string
  onCrop: (blob: Blob, originalHash: string) => void
  onCancel: () => void
}

export const ImageCropper = ({ imageUrl, imageHash, onCrop, onCancel }: CropperOptions) => {
  const container = document.createElement("div")
  container.className = "cropper-container"

  // State
  let rotation = 0
  let cropRect: CropRect = { x: 0, y: 0, width: 0, height: 0 }
  let image: HTMLImageElement | null = null
  let imageWidth = 0
  let imageHeight = 0
  let displayScale = 1

  // Drag state
  let isDragging = false
  let dragHandle: string | null = null
  let dragStartX = 0
  let dragStartY = 0
  let dragStartCrop: CropRect = { x: 0, y: 0, width: 0, height: 0 }
  let isMoving = false

  // DOM elements
  const imageContainer = document.createElement("div")
  imageContainer.className = "cropper-image-container"

  // Wrapper for canvas + overlay to align them
  const canvasWrapper = document.createElement("div")
  canvasWrapper.className = "cropper-canvas-wrapper"

  const canvas = document.createElement("canvas")
  canvas.className = "cropper-canvas"
  const ctx = canvas.getContext("2d")!

  const overlay = document.createElement("div")
  overlay.className = "cropper-overlay"

  const cropBox = document.createElement("div")
  cropBox.className = "cropper-crop-box"
  
  // Add corner handles
  const handleNW = document.createElement("div")
  handleNW.className = "handle-nw"
  const handleNE = document.createElement("div")
  handleNE.className = "handle-ne"
  const handleSW = document.createElement("div")
  handleSW.className = "handle-sw"
  const handleSE = document.createElement("div")
  handleSE.className = "handle-se"
  cropBox.appendChild(handleNW)
  cropBox.appendChild(handleNE)
  cropBox.appendChild(handleSW)
  cropBox.appendChild(handleSE)

  // Controls
  const controls = document.createElement("div")
  controls.className = "cropper-controls"
  controls.innerHTML = `
    <div class="cropper-rotate">
      <div class="cropper-rotate-label">Rotate</div>
      <div class="cropper-rotate-buttons">
        <button class="cropper-btn rotate-left" title="Rotate left 90°">↺ 90°</button>
        <button class="cropper-btn rotate-right" title="Rotate right 90°">↻ 90°</button>
      </div>
      <input type="range" class="cropper-slider" min="-180" max="180" value="0" step="1">
      <div class="cropper-angle-display">0°</div>
    </div>
    <button class="cropper-btn cropper-upload">Upload Cropped Image</button>
  `

  const rotateLeftBtn = controls.querySelector(".rotate-left") as HTMLButtonElement
  const rotateRightBtn = controls.querySelector(".rotate-right") as HTMLButtonElement
  const rotateSlider = controls.querySelector(".cropper-slider") as HTMLInputElement
  const angleDisplay = controls.querySelector(".cropper-angle-display") as HTMLDivElement
  const uploadBtn = controls.querySelector(".cropper-upload") as HTMLButtonElement

  // Initialize
  const init = () => {
    container.appendChild(imageContainer)
    container.appendChild(controls)
    imageContainer.appendChild(canvasWrapper)
    canvasWrapper.appendChild(canvas)
    canvasWrapper.appendChild(overlay)
    overlay.appendChild(cropBox)

    loadImage()
    setupEventListeners()
  }

  const loadImage = () => {
    image = new Image()
    image.onload = () => {
      imageWidth = image!.naturalWidth
      imageHeight = image!.naturalHeight
      fitImageToContainer()
      drawImage()
      initCropRect()
      updateCropBox()
    }
    image.src = imageUrl
  }

  const fitImageToContainer = () => {
    // Use fixed max dimensions since container might not be in DOM yet
    const maxWidth = 500
    const maxHeight = 500
    const rotatedW = getRotatedWidth()
    const rotatedH = getRotatedHeight()
    
    if (rotatedW === 0 || rotatedH === 0) return
    
    const scaleX = maxWidth / rotatedW
    const scaleY = maxHeight / rotatedH
    displayScale = Math.min(scaleX, scaleY, 1)

    canvas.width = Math.round(rotatedW * displayScale)
    canvas.height = Math.round(rotatedH * displayScale)
  }

  const getRotatedWidth = () => {
    const rad = (rotation * Math.PI) / 180
    return Math.abs(imageWidth * Math.cos(rad)) + Math.abs(imageHeight * Math.sin(rad))
  }

  const getRotatedHeight = () => {
    const rad = (rotation * Math.PI) / 180
    return Math.abs(imageWidth * Math.sin(rad)) + Math.abs(imageHeight * Math.cos(rad))
  }

  const drawImage = () => {
    const rad = (rotation * Math.PI) / 180
    const w = canvas.width
    const h = canvas.height

    ctx.clearRect(0, 0, w, h)
    ctx.save()
    ctx.translate(w / 2, h / 2)
    ctx.rotate(rad)
    ctx.drawImage(image!, -imageWidth * displayScale / 2, -imageHeight * displayScale / 2, imageWidth * displayScale, imageHeight * displayScale)
    ctx.restore()
  }

  const initCropRect = () => {
    const padding = 0.1
    cropRect = {
      x: canvas.width * padding,
      y: canvas.height * padding,
      width: canvas.width * (1 - 2 * padding),
      height: canvas.height * (1 - 2 * padding),
    }
  }

  const updateCropBox = () => {
    cropBox.style.left = cropRect.x + "px"
    cropBox.style.top = cropRect.y + "px"
    cropBox.style.width = cropRect.width + "px"
    cropBox.style.height = cropRect.height + "px"
  }

  const setupEventListeners = () => {
    // Rotate buttons
    rotateLeftBtn.addEventListener("click", () => {
      rotation = (rotation - 90) % 360
      if (rotation < -180) rotation += 360
      rotateSlider.value = String(rotation)
      angleDisplay.textContent = rotation + "°"
      fitImageToContainer()
      drawImage()
      initCropRect()
      updateCropBox()
    })

    rotateRightBtn.addEventListener("click", () => {
      rotation = (rotation + 90) % 360
      if (rotation > 180) rotation -= 360
      rotateSlider.value = String(rotation)
      angleDisplay.textContent = rotation + "°"
      fitImageToContainer()
      drawImage()
      initCropRect()
      updateCropBox()
    })

    rotateSlider.addEventListener("input", () => {
      rotation = parseInt(rotateSlider.value)
      angleDisplay.textContent = rotation + "°"
      fitImageToContainer()
      drawImage()
      // Keep crop rect but clamp to canvas bounds
      clampCropRect()
      updateCropBox()
    })

    // Crop box drag handles
    cropBox.addEventListener("mousedown", (e) => {
      e.preventDefault()
      const rect = overlay.getBoundingClientRect()
      const x = e.clientX - rect.left
      const y = e.clientY - rect.top

      // Check if near a handle (corners)
      const handleSize = 12
      const handles = [
        { name: "nw", x: cropRect.x, y: cropRect.y },
        { name: "ne", x: cropRect.x + cropRect.width, y: cropRect.y },
        { name: "sw", x: cropRect.x, y: cropRect.y + cropRect.height },
        { name: "se", x: cropRect.x + cropRect.width, y: cropRect.y + cropRect.height },
      ]

      for (const handle of handles) {
        if (Math.abs(x - handle.x) < handleSize && Math.abs(y - handle.y) < handleSize) {
          isDragging = true
          dragHandle = handle.name
          dragStartX = x
          dragStartY = y
          dragStartCrop = { ...cropRect }
          return
        }
      }

      // Check if inside crop box for moving
      if (x >= cropRect.x && x <= cropRect.x + cropRect.width &&
          y >= cropRect.y && y <= cropRect.y + cropRect.height) {
        isDragging = true
        isMoving = true
        dragHandle = null
        dragStartX = x
        dragStartY = y
        dragStartCrop = { ...cropRect }
      }
    })

    document.addEventListener("mousemove", (e) => {
      if (!isDragging) return
      e.preventDefault()

      const rect = overlay.getBoundingClientRect()
      const x = e.clientX - rect.left
      const y = e.clientY - rect.top
      const dx = x - dragStartX
      const dy = y - dragStartY

      if (isMoving) {
        cropRect.x = Math.max(0, Math.min(canvas.width - dragStartCrop.width, dragStartCrop.x + dx))
        cropRect.y = Math.max(0, Math.min(canvas.height - dragStartCrop.height, dragStartCrop.y + dy))
      } else if (dragHandle) {
        const minSize = 20

        switch (dragHandle) {
          case "nw":
            cropRect.x = Math.max(0, dragStartCrop.x + dx)
            cropRect.y = Math.max(0, dragStartCrop.y + dy)
            cropRect.width = Math.max(minSize, dragStartCrop.width - (cropRect.x - dragStartCrop.x))
            cropRect.height = Math.max(minSize, dragStartCrop.height - (cropRect.y - dragStartCrop.y))
            break
          case "ne":
            cropRect.width = Math.max(minSize, Math.min(canvas.width - dragStartCrop.x, dragStartCrop.width + dx))
            cropRect.y = Math.max(0, dragStartCrop.y + dy)
            cropRect.height = Math.max(minSize, dragStartCrop.height - (cropRect.y - dragStartCrop.y))
            break
          case "sw":
            cropRect.x = Math.max(0, dragStartCrop.x + dx)
            cropRect.width = Math.max(minSize, dragStartCrop.width - (cropRect.x - dragStartCrop.x))
            cropRect.height = Math.max(minSize, Math.min(canvas.height - dragStartCrop.y, dragStartCrop.height + dy))
            break
          case "se":
            cropRect.width = Math.max(minSize, Math.min(canvas.width - dragStartCrop.x, dragStartCrop.width + dx))
            cropRect.height = Math.max(minSize, Math.min(canvas.height - dragStartCrop.y, dragStartCrop.height + dy))
            break
        }
      }

      updateCropBox()
    })

    document.addEventListener("mouseup", () => {
      isDragging = false
      isMoving = false
      dragHandle = null
    })

    // Touch support
    cropBox.addEventListener("touchstart", (e) => {
      e.preventDefault()
      const touch = e.touches[0]
      const rect = overlay.getBoundingClientRect()
      const x = touch.clientX - rect.left
      const y = touch.clientY - rect.top

      // Default to moving if inside crop box
      if (x >= cropRect.x && x <= cropRect.x + cropRect.width &&
          y >= cropRect.y && y <= cropRect.y + cropRect.height) {
        isDragging = true
        isMoving = true
        dragHandle = null
        dragStartX = x
        dragStartY = y
        dragStartCrop = { ...cropRect }
      }
    })

    document.addEventListener("touchmove", (e) => {
      if (!isDragging) return
      e.preventDefault()

      const touch = e.touches[0]
      const rect = overlay.getBoundingClientRect()
      const x = touch.clientX - rect.left
      const y = touch.clientY - rect.top
      const dx = x - dragStartX
      const dy = y - dragStartY

      if (isMoving) {
        cropRect.x = Math.max(0, Math.min(canvas.width - dragStartCrop.width, dragStartCrop.x + dx))
        cropRect.y = Math.max(0, Math.min(canvas.height - dragStartCrop.height, dragStartCrop.y + dy))
        updateCropBox()
      }
    })

    document.addEventListener("touchend", () => {
      isDragging = false
      isMoving = false
      dragHandle = null
    })

    // Upload button
    uploadBtn.addEventListener("click", processAndUpload)

    // Handle window resize
    window.addEventListener("resize", () => {
      fitImageToContainer()
      drawImage()
      clampCropRect()
      updateCropBox()
    })
  }

  const clampCropRect = () => {
    cropRect.x = Math.max(0, Math.min(cropRect.x, canvas.width - 20))
    cropRect.y = Math.max(0, Math.min(cropRect.y, canvas.height - 20))
    cropRect.width = Math.min(cropRect.width, canvas.width - cropRect.x)
    cropRect.height = Math.min(cropRect.height, canvas.height - cropRect.y)
  }

  const processAndUpload = async () => {
    uploadBtn.disabled = true
    uploadBtn.textContent = "Processing..."

    try {
      const blob = await exportCroppedImage()
      onCrop(blob, imageHash)
    } catch (err) {
      console.error("Export failed:", err)
      alert("Failed to process image")
    } finally {
      uploadBtn.disabled = false
      uploadBtn.textContent = "Upload Cropped Image"
    }
  }

  const exportCroppedImage = (): Promise<Blob> => {
    return new Promise((resolve, reject) => {
      // Create output canvas at original resolution
      const outputCanvas = document.createElement("canvas")
      const outputCtx = outputCanvas.getContext("2d")!

      // Calculate crop rect in original image coordinates
      const scaleX = getRotatedWidth() / canvas.width
      const scaleY = getRotatedHeight() / canvas.height

      const cropX = cropRect.x * scaleX
      const cropY = cropRect.y * scaleY
      const cropW = cropRect.width * scaleX
      const cropH = cropRect.height * scaleY

      // Set output size to crop dimensions
      outputCanvas.width = Math.round(cropW)
      outputCanvas.height = Math.round(cropH)

      // Draw rotated and cropped image
      const rad = (rotation * Math.PI) / 180
      const centerX = getRotatedWidth() / 2
      const centerY = getRotatedHeight() / 2

      outputCtx.save()
      outputCtx.translate(outputCanvas.width / 2 - cropX - cropW / 2, outputCanvas.height / 2 - cropY - cropH / 2)
      outputCtx.translate(centerX, centerY)
      outputCtx.rotate(rad)
      outputCtx.drawImage(image!, -imageWidth / 2, -imageHeight / 2, imageWidth, imageHeight)
      outputCtx.restore()

      outputCanvas.toBlob((blob) => {
        if (blob) {
          resolve(blob)
        } else {
          reject(new Error("Failed to create blob"))
        }
      }, "image/jpeg", 0.92)
    })
  }

  const getElement = () => container

  init()

  return { getElement }
}
