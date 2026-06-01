import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, button, img, input, p, form } = van.tags

const UploadPage = () => {
  const photo = van.state<File | null>(null)
  const preview = van.state<string | null>(null)
  const uploading = van.state(false)
  const error = van.state("")

  const handleFileSelect = (e: Event) => {
    const input = e.target as HTMLInputElement
    if (input.files && input.files[0]) {
      photo.val = input.files[0]
      preview.val = URL.createObjectURL(photo.val)
    }
  }

  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      photo.val = e.dataTransfer.files[0]
      preview.val = URL.createObjectURL(photo.val)
    }
  }

  const handleDragOver = (e: Event) => {
    e.preventDefault()
  }

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    if (!photo.val) {
      error.val = "Please select a photo"
      return
    }

    uploading.val = true
    error.val = ""

    try {
      const formData = new FormData()
      formData.append("photo", photo.val)

      const token = localStorage.getItem("token")
      const response = await fetch("/api/receipts/upload", {
        method: "POST",
        headers: {
          "Authorization": `Bearer ${token}`,
        },
        body: formData,
      })

      if (!response.ok) {
        const data = await response.json()
        throw new Error(data.error || "Upload failed")
      }

      const proposal = await response.json()
      navigate(`/proposals/${proposal.proposalId}`)
    } catch (err) {
      error.val = err instanceof Error ? err.message : "Upload failed"
    } finally {
      uploading.val = false
    }
  }

  return div({ class: "upload-page" },
    div({ class: "page-header" },
      h1("Upload Receipt"),
      button({ onclick: () => navigate("/receipts") }, "Back"),
    ),
    form({ class: "upload-form", onsubmit: handleSubmit },
      div({
        class: "dropzone",
        ondrop: handleDrop,
        ondragover: handleDragOver,
        onclick: () => document.getElementById("file-input")?.click(),
      },
        () => preview.val
          ? img({ src: preview.val, class: "preview" })
          : div({ class: "dropzone-text" },
              p("Drag & drop receipt photo here"),
              p({ class: "dropzone-hint" }, "or click to select"),
            ),
      ),
      input({
        id: "file-input",
        type: "file",
        accept: "image/*",
        capture: "environment",
        style: "display: none",
        onchange: handleFileSelect,
      }),
      () => error.val ? p({ class: "error" }, error.val) : "",
      button({
        type: "submit",
        disabled: uploading,
        class: "upload-btn",
      }, uploading.val ? "Uploading..." : "Upload & Parse"),
    ),
  )
}

export default UploadPage
