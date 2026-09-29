import { createSignal, onCleanup, Show } from "solid-js"
import ImageCropperView from "../components/cropper.tsx"
import { api, navigate } from "../lib/api.ts"

// Compute SHA-256 hash of a file
const computeFileHash = async (file: File): Promise<string> => {
  const buffer = await file.arrayBuffer()
  const hashBuffer = await crypto.subtle.digest("SHA-256", buffer)
  const hashArray = Array.from(new Uint8Array(hashBuffer))
  return hashArray.map((b) => b.toString(16).padStart(2, "0")).join("")
}

// Platform-specific paste shortcut for the dropzone hint
const pasteShortcut = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘V" : "Ctrl+V"

export default function UploadPage() {
  const [preview, setPreview] = createSignal<string | null>(null)
  const [imageHash, setImageHash] = createSignal("")
  // Keep the original File around so the Skip-Crop path can upload
  // the raw bytes without re-asking the user to pick the file. The
  // cropper only has the blob URL (for display), not the File.
  const [originalFile, setOriginalFile] = createSignal<File | null>(null)
  const [uploading, setUploading] = createSignal(false)
  const [error, setError] = createSignal("")

  const processFile = async (file: File) => {
    setPreview(URL.createObjectURL(file))
    setImageHash(await computeFileHash(file))
    setOriginalFile(file)
    setError("")
  }

  const handleFileSelect = async (e: Event) => {
    const fileInput = e.target as HTMLInputElement
    if (fileInput.files && fileInput.files[0]) {
      await processFile(fileInput.files[0]!)
    }
  }

  const handleDrop = async (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      await processFile(e.dataTransfer.files[0]!)
    }
  }

  const handleDragOver = (e: Event) => {
    e.preventDefault()
  }

  // Global paste handler — Cmd+V / Ctrl+V anywhere on the page.
  // Ignored once a preview is set (the cropper has its own UI).
  // If the clipboard has no image, do nothing; default paste proceeds.
  const handlePaste = async (e: ClipboardEvent) => {
    if (preview()) return
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
  onCleanup(() => document.removeEventListener("paste", handlePaste))

  const uploadError = (err: any): string => {
    // Duplicate-image hint.
    if (err.message?.includes("duplicate_image") || err.message?.includes("already uploaded")) {
      return "This image was already uploaded. Please use a different photo."
    }
    return err instanceof Error ? err.message : "Upload failed"
  }

  const handleCrop = async (blob: Blob, originalHash: string) => {
    setUploading(true)
    setError("")
    try {
      const file = new File([blob], "receipt.jpg", { type: "image/jpeg" })
      const formData = new FormData()
      formData.append("photo", file)
      formData.append("originalHash", originalHash)
      const data = await api.postFormData("/receipts/upload", formData)
      navigate(`/proposals/${data.id}`)
    } catch (err: any) {
      setError(uploadError(err))
    } finally {
      setUploading(false)
    }
  }

  // Skip-crop path: upload the original File as-is. Same server
  // contract as handleCrop (multipart `photo` + `originalHash`).
  const handleSkip = async (originalHash: string) => {
    const file = originalFile()
    if (!file) return
    setUploading(true)
    setError("")
    try {
      const formData = new FormData()
      formData.append("photo", file)
      formData.append("originalHash", originalHash)
      const data = await api.postFormData("/receipts/upload", formData)
      navigate(`/proposals/${data.id}`)
    } catch (err: any) {
      setError(uploadError(err))
    } finally {
      setUploading(false)
    }
  }

  const cancelCrop = () => {
    const url = preview()
    if (url) URL.revokeObjectURL(url)
    setPreview(null)
    setOriginalFile(null)
  }

  void uploading

  return (
    <div class="upload-page">
      <div class="page-header">
        <h1>Upload Receipt</h1>
        <div class="page-header-actions">
          <button onClick={() => navigate("/receipts/manual")} class="btn-secondary">
            Enter Manually
          </button>
          <button onClick={() => navigate("/receipts")}>Back</button>
        </div>
      </div>
      <div class="upload-form">
        {/* Cropper binds image URL + hash at construction, so mount
            only when BOTH are ready — mounting on preview alone
            would capture imageHash="" and send an empty
            originalHash to the server (breaking dedupe). */}
        <Show
          when={preview()}
          fallback={
            <div
              class="dropzone"
              onDrop={handleDrop}
              onDragOver={handleDragOver}
              onClick={() => {
                const fileInput = document.getElementById("file-input") as HTMLInputElement | null
                fileInput?.click()
              }}
            >
              <div class="dropzone-text">
                <p>Drag & drop, paste ({pasteShortcut}), or click to upload</p>
                <p class="dropzone-hint">or select a file</p>
              </div>
            </div>
          }
        >
          <Show
            when={imageHash()}
            fallback={<div class="muted">Preparing…</div>}
          >
            {(hash) => (
              <ImageCropperView
                imageUrl={preview()!}
                imageHash={hash()}
                onCrop={handleCrop}
                onSkip={handleSkip}
                onCancel={cancelCrop}
              />
            )}
          </Show>
        </Show>
        <input
          id="file-input"
          type="file"
          accept="image/*"
          capture="environment"
          style="display: none"
          onChange={handleFileSelect}
        />
        <Show when={error()}>
          <p class="error">{error()}</p>
        </Show>
      </div>
    </div>
  )
}
