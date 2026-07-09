// Photo helpers for receipt/proposal pages.
//
// The /api/photos/{id} endpoint requires the Bearer token (unlike the
// GCS-served PhotoURL embedded in enriched DTOs, which is public). This
// helper wraps fetch with the auth header and converts the response to
// a blob URL that the <img> tag can use directly.
//
// Cache strategy: LRU-bounded (PHOTO_CACHE_MAX entries). Map iteration
// order is insertion order in JavaScript, so the oldest entry is at the
// front. When the cache exceeds the cap, we evict the oldest entry
// (revoking its blob URL to free memory) before adding a new one.
// On a cache hit we re-insert the entry to mark it as recent.
//
// Worst-case memory: PHOTO_CACHE_MAX entries × ~200KB = ~10MB. Family
// scale visits are typically < 20 unique photos per session, well
// within the cap.

const photoUrlCache = new Map<string, string>()

// Max number of blob URLs to keep in memory. Each is a full-size JPEG
// (~200KB). Tune based on actual photo sizes if needed.
const PHOTO_CACHE_MAX = 50

export const fetchPhotoUrl = async (receiptId: number | string): Promise<string> => {
  const key = String(receiptId)
  const cached = photoUrlCache.get(key)
  if (cached) {
    // Mark as recent: delete + re-insert moves the entry to the back of
    // the iteration order.
    photoUrlCache.delete(key)
    photoUrlCache.set(key, cached)
    return cached
  }

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

  // Evict oldest entries if over the cap. Map iteration is insertion
  // order, so the first key is the oldest.
  while (photoUrlCache.size > PHOTO_CACHE_MAX) {
    const oldestKey = photoUrlCache.keys().next().value
    if (oldestKey === undefined) break
    const oldestUrl = photoUrlCache.get(oldestKey)
    if (oldestUrl) URL.revokeObjectURL(oldestUrl)
    photoUrlCache.delete(oldestKey)
  }

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
