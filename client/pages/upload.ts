import van from "vanjs-core"
import { api, navigate } from "../main"
import { ImageCropper } from "../components/cropper"

const { div, h1, button, p, input } = van.tags

const UploadPage = () => {
  const preview = van.state<string | null>(null)
  const uploading = van.state(false)
  const error = van.state("")
  const cropperContainer = van.state<HTMLDivElement | null>(null)

  const handleFileSelect = (e: Event) => {
    const input = e.target as HTMLInputElement
    if (input.files && input.files[0]) {
      preview.val = URL.createObjectURL(input.files[0])
      error.val = ""
    }
  }

  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      preview.val = URL.createObjectURL(e.dataTransfer.files[0])
      error.val = ""
    }
  }

  const handleDragOver = (e: Event) => {
    e.preventDefault()
  }

  const handleCrop = async (blob: Blob) => {
    uploading.val = true
    error.val = ""

    try {
      const file = new File([blob], "receipt.jpg", { type: "image/jpeg" })
      const formData = new FormData()
      formData.append("photo", file)

      const data = await api.postFormData("/receipts/upload", formData)
      navigate(`/proposals/${data.id}`)
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Upload failed"
    } finally {
      uploading.val = false
    }
  }

  const initCropper = (imageUrl: string) => {
    const cropper = ImageCropper({
      imageUrl,
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
        ? initCropper(preview.val)
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
