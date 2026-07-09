package api

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

func (r *Router) handleGetPhoto(w http.ResponseWriter, req *http.Request) {
	// GET /api/photos/{id} - works for both receipts and proposals
	idStr := req.PathValue("id")
	if idStr == "" {
		// Try to extract from URL path
		parts := strings.Split(req.URL.Path, "/")
		if len(parts) >= 4 {
			idStr = parts[3]
		}
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ID")
		return
	}

	// Try receipt first, then proposal
	var photoURL string
	receipt, err := r.store.GetReceipt(id)
	if err == nil {
		photoURL = receipt.PhotoURL
	} else {
		proposal, err := r.store.GetProposal(id)
		if err == nil {
			photoURL = proposal.PhotoURL
		}
	}

	if photoURL == "" {
		writeError(w, http.StatusNotFound, "no photo found")
		return
	}

	// Try local cache first
	var data []byte
	if r.photoCache != nil {
		data, err = r.photoCache.Get(req.Context(), photoURL)
	}

	// If cache miss, get from GCloud
	if data == nil && r.photoStore != nil {
		data, err = r.photoStore.Get(req.Context(), photoURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get photo")
			return
		}

		// Cache locally
		if r.photoCache != nil {
			r.photoCache.Set(req.Context(), photoURL, data)
		}
	}

	if data == nil {
		writeError(w, http.StatusNotFound, "photo not found")
		return
	}

	// If the client requests a thumbnail (?size=thumb), resize the
	// decoded image to a max edge of 200px and re-encode as JPEG.
	// The full-size bytes are still cached on disk so this is a CPU
	// cost only (the GCloud round-trip is the expensive part, and
	// it's already done by the time we get here).
	if req.URL.Query().Get("size") == "thumb" {
		data = resizePhoto(data, 200)
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// resizePhoto decodes a JPEG/PNG, scales to fit within `max` pixels
// on the longest edge, and re-encodes as JPEG at 80% quality. Returns
// the original bytes on decode/encode failure rather than 500ing — a
// broken thumbnail shouldn't break the whole list page.
func resizePhoto(data []byte, max int) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("photo: decode failed: %v", err)
		return data
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= max && h <= max {
		return data
	}
	ratio := float64(max) / float64(w)
	if h > w {
		ratio = float64(max) / float64(h)
	}
	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		log.Printf("photo: encode failed: %v", err)
		return data
	}
	return buf.Bytes()
}
