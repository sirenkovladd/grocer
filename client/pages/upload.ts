import van from "vanjs-core"
import { api, navigate } from "../main"
import { ImageCropper } from "../components/cropper"

const { div, h1, button, p, input } = van.tags

// Compute SHA-256 hash of a file
const computeFileHash = async (file: File): Promise<string> => {
  const buffer = await file.arrayBuffer()
  const hashBuffer = await crypto.subtle.digest("SHA-256", buffer)
  const hashArray = Array.from(new Uint8Array(hashBuffer))
  return hashArray.map(b => b.toString(16).padStart(2, "0")).join("")
}

// Platform-specific paste shortcut for the dropzone hint
const pasteShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘V" : "Ctrl+V"

const UploadPage = () => {
  const preview = van.state<string | null>(null)
  const imageHash = van.state<string>("")
  // Keep the original File around so the Skip-Crop path can upload
  // the raw bytes without re-asking the user to pick the file. The
  // cropper itself only has the blob URL (for display), not the
  // original File, so we track it here on the page.
  const originalFile = van.state<File | null>(null)
  const uploading = van.state(false)
  const error = van.state("")
  const cropperContainer = van.state<HTMLDivElement | null>(null)

  const processFile = async (file: File) => {
    preview.val = URL.createObjectURL(file)
    imageHash.val = await computeFileHash(file)
    originalFile.val = file
    error.val = ""
  }

  const handleFileSelect = async (e: Event) => {
    const fileInput = e.target as HTMLInputElement
    if (fileInput.files && fileInput.files[0]) {
      await processFile(fileInput.files[0])
    }
  }

  const handleDrop = async (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      await processFile(e.dataTransfer.files[0])
    }
  }

  const handleDragOver = (e: Event) => {
    e.preventDefault()
  }

  // Global paste handler — fires for Cmd+V / Ctrl+V anywhere on the page.
  // The cropper has its own UI, so we ignore paste once a preview is set.
  // If the clipboard has no image, do nothing and let default paste proceed.
  const handlePaste = async (e: ClipboardEvent) => {
    if (preview.val) return
    const items = e.clipboardData?.items
    if (!items) return
    for (const item of Array.from(items)) {
      if (item.kind === "file" && item.type.startsWith("image/")) {
        const file = item.getAsFile()
        if (file) {
          e.preventDefault()
          await processFile(file)
        }
        return
      }
    }
  }
  document.addEventListener("paste", handlePaste)

  const handleCrop = async (blob: Blob, originalHash: string) => {
    uploading.val = true
    error.val = ""

    try {
      const file = new File([blob], "receipt.jpg", { type: "image/jpeg" })
      const formData = new FormData()
      formData.append("photo", file)
      formData.append("originalHash", originalHash)

      const data = await api.postFormData("/receipts/upload", formData)
      navigate(`/proposals/${data.id}`)
    } catch (err: any) {
      // Check if it's a duplicate image error
      if (err.message?.includes("duplicate_image") || err.message?.includes("already uploaded")) {
        error.val = "This image was already uploaded. Please use a different photo."
      } else {
        error.val = err instanceof Error ? err.message : "Upload failed"
      }
    } finally {
      uploading.val = false
    }
  }

  // Skip-crop path: upload the original File as-is, with no
  // cropping or rotation. Same server contract as handleCrop
  // (multipart form with `photo` and `originalHash`).
  const handleSkip = async (originalHash: string) => {
    const file = originalFile.val
    if (!file) return
    uploading.val = true
    error.val = ""
    try {
      const formData = new FormData()
      formData.append("photo", file)
      formData.append("originalHash", originalHash)
      const data = await api.postFormData("/receipts/upload", formData)
      navigate(`/proposals/${data.id}`)
    } catch (err: any) {
      if (err.message?.includes("duplicate_image") || err.message?.includes("already uploaded")) {
        error.val = "This image was already uploaded. Please use a different photo."
      } else {
        error.val = err instanceof Error ? err.message : "Upload failed"
      }
    } finally {
      uploading.val = false
    }
  }

  const initCropper = (imageUrl: string, hash: string) => {
    const cropper = ImageCropper({
      imageUrl,
      imageHash: hash,
      onCrop: handleCrop,
      onSkip: handleSkip,
      onCancel: () => {
        preview.val = null
        cropperContainer.val = null
        originalFile.val = null
      },
    })
    return cropper.getElement()
  }

  const renderDropzone = () => {
    const dropzone = document.createElement("div")
    dropzone.className = "dropzone"
    dropzone.addEventListener("drop", handleDrop)
    dropzone.addEventListener("dragover", handleDragOver)
    dropzone.addEventListener("click", () => {
      const fileInput = document.getElementById("file-input") as HTMLInputElement
      fileInput?.click()
    })
    dropzone.innerHTML = `
      <div class="dropzone-text">
        <p>Drag &amp; drop, paste (${pasteShortcut}), or click to upload</p>
        <p class="dropzone-hint">or select a file</p>
      </div>
    `
    return dropzone
  }

  return div({ class: "upload-page" },
    div({ class: "page-header" },
      h1("Upload Receipt"),
      div({ class: "page-header-actions" },
        button({
          onclick: () => navigate("/receipts/manual"),
          class: "btn-secondary",
        }, "Enter Manually"),
        button({ onclick: () => navigate("/receipts") }, "Back"),
      ),
    ),
    div({ class: "upload-form" },
      () => preview.val
        ? initCropper(preview.val, imageHash.val)
        : renderDropzone(),
      input({
        id: "file-input",
        type: "file",
        accept: "image/*",
        capture: "environment",
        style: "display: none",
        onchange: handleFileSelect,
      }),
      () => error.val ? p({ class: "error" }, error.val) : "",
    ),
  )
}

export default UploadPage
