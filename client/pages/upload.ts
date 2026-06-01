import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, h1, button, img, input, p, span, ul, li } = van.tags

interface ParsedItem {
  parsedName: string
  quantity: number
  unitPriceCents: number
}

interface ParseEvent {
  type: "progress" | "item" | "done" | "error"
  message?: string
  item?: ParsedItem
  index?: number
  proposal?: { proposalId: number }
}

const UploadPage = () => {
  const photo = van.state<File | null>(null)
  const preview = van.state<string | null>(null)
  const uploading = van.state(false)
  const error = van.state("")
  const status = van.state("")
  const items = van.state<ParsedItem[]>([])

  const handleFileSelect = (e: Event) => {
    const input = e.target as HTMLInputElement
    if (input.files && input.files[0]) {
      photo.val = input.files[0]
      preview.val = URL.createObjectURL(photo.val)
      items.val = []
      status.val = ""
      error.val = ""
    }
  }

  const handleDrop = (e: DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer?.files && e.dataTransfer.files[0]) {
      photo.val = e.dataTransfer.files[0]
      preview.val = URL.createObjectURL(photo.val)
      items.val = []
      status.val = ""
      error.val = ""
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
    items.val = []
    status.val = "Uploading..."

    try {
      const formData = new FormData()
      formData.append("photo", photo.val)

      const token = localStorage.getItem("token")
      const response = await fetch("/api/receipts/upload/stream", {
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

      console.log("[SSE] Starting stream upload...")
      const reader = response.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ""

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        console.log("[SSE] Received chunk, buffer length:", buffer.length)

        // Parse SSE events from buffer
        const parts = buffer.split("\n\n")
        buffer = parts.pop()! // keep incomplete part

        for (const part of parts) {
          let eventType = ""
          let dataStr = ""
          for (const line of part.split("\n")) {
            if (line.startsWith("event: ")) {
              eventType = line.slice(7)
            } else if (line.startsWith("data: ")) {
              dataStr = line.slice(6)
            }
          }
          if (!eventType || !dataStr) continue

          console.log("[SSE] Event:", eventType, dataStr.slice(0, 200))

          try {
            const event: ParseEvent = JSON.parse(dataStr)

            if (event.type === "progress" && event.message) {
              status.val = event.message
            } else if (event.type === "item" && event.item) {
              items.val = [...items.val, event.item]
            } else if (event.type === "done" && event.proposal) {
              status.val = "Done!"
              navigate(`/proposals/${event.proposal.proposalId}`)
              return
            } else if (event.type === "error") {
              throw new Error(event.message || "Parse failed")
            }
          } catch (parseErr) {
            if (parseErr instanceof Error && parseErr.message !== "Parse failed") {
              console.warn("SSE parse error:", parseErr)
            } else if (parseErr instanceof Error) {
              throw parseErr
            }
          }
        }
      }
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
    div({ class: "upload-form" },
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
      () => status.val && !error.val ? p({ class: "upload-status" }, status.val) : "",
      () => {
        if (items.val.length === 0) return ""
        return div({ class: "streaming-items" },
          ul(
            ...items.val.map((it, i) =>
              li({ class: "streaming-item" },
                span({ class: "item-name" }, it.parsedName),
                span({ class: "item-qty" }, ` ×${it.quantity}`),
                span({ class: "item-price" }, ` $${(it.unitPriceCents / 100).toFixed(2)}`),
              )
            )
          )
        )
      },
      button({
        type: "button",
        disabled: uploading,
        class: "upload-btn",
        onclick: handleSubmit,
      }, uploading.val ? "Parsing..." : "Upload & Parse"),
    ),
  )
}

export default UploadPage
