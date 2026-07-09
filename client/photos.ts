// Photo helpers for receipt/proposal pages.
//
// The /api/photos/{id} endpoint requires the Bearer token (unlike the
// GCS-served PhotoURL embedded in enriched DTOs, which is public). This
// helper wraps fetch with the auth header and converts the response to
// a blob URL that the <img> tag can use directly.
//
// IMPORTANT: blob URLs must be revoked with URL.revokeObjectURL when no
// longer needed to avoid memory leaks. Callers that swap photos on
// re-render should track the previous URL and revoke it before setting
// a new one.

const photoUrlCache = new Map<string, string>()

export const fetchPhotoUrl = async (receiptId: number | string): Promise<string> => {
  const key = String(receiptId)
  const cached = photoUrlCache.get(key)
  if (cached) return cached

  const token = localStorage.getItem("token")
  const response = await fetch(`/api/photos/${key}`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!response.ok) {
    throw new Error(`Failed to load photo: HTTP ${response.status}`)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  photoUrlCache.set(key, url)
  return url
}

// Revoke the cached blob URL for a given receipt/proposal ID. Use when
// the photo is no longer displayed (e.g. on page unmount).
export const revokePhotoUrl = (receiptId: number | string) => {
  const key = String(receiptId)
  const url = photoUrlCache.get(key)
  if (url) {
    URL.revokeObjectURL(url)
    photoUrlCache.delete(key)
  }
}
