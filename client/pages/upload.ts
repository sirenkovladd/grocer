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

const UploadPage = () => {
  const preview = van.state<string | null>(null)
  const imageHash = van.state<string>("")
  const uploading = van.state(false)
  const error = van.state("")
  const cropperContainer = van.state<HTMLDivElement | null>(null)

  const handleFileSelect = async (e: Event) => {
    const fileInput = e.target as HTMLInputElement
    if (fileInput.files && fileInput.files[0]) {
      const file = fileInput.files[0]
      preview.val = URL.createObjectURL(file)
      imageHash.val = await computeFileHash(file)
      error.val = ""
    }
  }

  const handleDrop = async (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      const file = e.dataTransfer.files[0]
      preview.val = URL.createObjectURL(file)
      imageHash.val = await computeFileHash(file)
      error.val = ""
    }
  }

  const handleDragOver = (e: Event) => {
    e.preventDefault()
  }

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

  const initCropper = (imageUrl: string, hash: string) => {
    const cropper = ImageCropper({
      imageUrl,
      imageHash: hash,
      onCrop: handleCrop,
      onCancel: () => {
        preview.val = null
        cropperContainer.val = null
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
        <p>Drag & drop receipt photo here</p>
        <p class="dropzone-hint">or click to select</p>
      </div>
    `
    return dropzone
  }

  return div({ class: "upload-page" },
    div({ class: "page-header" },
      h1("Upload Receipt"),
      button({ onclick: () => navigate("/receipts") }, "Back"),
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
