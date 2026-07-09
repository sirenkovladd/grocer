// Photo helpers for receipt/proposal pages.
//
// The /api/photos/{id} endpoint is authenticated by the HttpOnly session
// cookie, which the browser auto-attaches to same-origin requests. No
// client-side token is needed. The GCS-served PhotoURL embedded in
// enriched DTOs is unauthenticated and public.
//
// Cache strategy: LRU-bounded (PHOTO_CACHE_MAX entries per size). Map
// iteration order is insertion order in JavaScript, so the oldest entry
// is at the front. When the cache exceeds the cap, we evict the oldest
// entry (revoking its blob URL to free memory) before adding a new one.
// On a cache hit we re-insert the entry to mark it as recent.
//
// The cache is split by size: a full-size image, a large variant, and
// a thumbnail of the same receipt are cached as separate keys, so they
// don't evict each other unpredictably.
//
// Worst-case memory: PHOTO_CACHE_MAX entries × ~200KB full-size +
// ~40KB large + ~5KB thumb = ~12MB at default cap. Family scale visits
// are typically < 20 unique photos per session, well within the cap.

type PhotoSize = "full" | "large" | "thumb"

const photoUrlCache = new Map<string, string>()

// Max number of blob URLs to keep in memory per size bucket. Each
// full-size blob is ~200KB; thumbs are ~5KB. Tune if photo sizes
// grow significantly.
const PHOTO_CACHE_MAX = 50

const cacheKey = (id: string | number, size: PhotoSize): string => {
  return `${size}:${id}`
}

export const fetchPhotoUrl = async (
  receiptId: number | string,
  size: PhotoSize = "full",
): Promise<string> => {
  const id = String(receiptId)
  const key = cacheKey(id, size)
  const cached = photoUrlCache.get(key)
  if (cached) {
    // Mark as recent: delete + re-insert moves the entry to the back of
    // the iteration order.
    photoUrlCache.delete(key)
    photoUrlCache.set(key, cached)
    return cached
  }

  const params = size === "full" ? "" : `?size=${size}`
  const response = await fetch(`/api/photos/${id}${params}`, {
    credentials: "same-origin",
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

// Revoke the cached blob URL for a given receipt/proposal ID. By
// default revokes both the full-size and thumbnail variants.
export const revokePhotoUrl = (receiptId: number | string, size?: PhotoSize) => {
  const id = String(receiptId)
  const sizes: PhotoSize[] = size ? [size] : ["full", "thumb"]
  for (const s of sizes) {
    const key = cacheKey(id, s)
    const url = photoUrlCache.get(key)
    if (url) {
      URL.revokeObjectURL(url)
      photoUrlCache.delete(key)
    }
  }
}
