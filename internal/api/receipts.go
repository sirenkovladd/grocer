package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
	"golang.org/x/image/draw"
)

func (r *Router) handleListReceipts(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	owner := req.URL.Query().Get("owner")
	category := req.URL.Query().Get("category")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Apply filters
	filtered := receipts
	if from != "" {
		fromDate, err := time.Parse("2006-01-02", from)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				if !time.Unix(receipt.Date, 0).Before(fromDate) {
					result = append(result, receipt)
				}
			}
			filtered = result
		}
	}

	if to != "" {
		toDate, err := time.Parse("2006-01-02", to)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				if !time.Unix(receipt.Date, 0).After(toDate) {
					result = append(result, receipt)
				}
			}
			filtered = result
		}
	}

	if owner != "" {
		ownerID, err := strconv.ParseUint(owner, 10, 64)
		if err == nil {
			var result []*domain.Receipt
			for _, receipt := range filtered {
				if receipt.OwnerID == ownerID {
					result = append(result, receipt)
				}
			}
			filtered = result
		}
	}

	if category != "" {
		categoryID, err := strconv.ParseUint(category, 10, 64)
		if err == nil {
			// Batch load all items to avoid N+1 queries
			itemMap := r.loadItemMap(filtered)

			var result []*domain.Receipt
			for _, receipt := range filtered {
				for _, item := range receipt.Items {
					if itemObj, ok := itemMap[item.ItemID]; ok && itemObj.CategoryID == categoryID {
						result = append(result, receipt)
						break
					}
				}
			}
			filtered = result
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

// loadItemMap batch loads all items referenced by receipts into a map for O(1) lookups
func (r *Router) loadItemMap(receipts []*domain.Receipt) map[uint64]*domain.Item {
	// Collect unique item IDs
	itemIDs := make(map[uint64]bool)
	for _, receipt := range receipts {
		for _, item := range receipt.Items {
			itemIDs[item.ItemID] = true
		}
	}

	// Batch load all items
	itemMap := make(map[uint64]*domain.Item)
	for itemID := range itemIDs {
		if itemObj, err := r.store.GetItem(itemID); err == nil {
			itemMap[itemID] = itemObj
		}
	}

	return itemMap
}

func (r *Router) handleGetReceipt(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid receipt ID")
		return
	}

	receipt, err := r.store.GetReceipt(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "receipt not found")
		return
	}

	writeJSON(w, http.StatusOK, receipt)
}

func (r *Router) handleUploadReceipt(w http.ResponseWriter, req *http.Request) {
	userID := r.getUserID(req)

	if err := req.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file too large")
		return
	}

	file, _, err := req.FormFile("photo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing photo")
		return
	}
	defer file.Close()

	photoData, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	// Get original image hash for duplicate detection
	originalHash := req.FormValue("originalHash")
	if originalHash != "" {
		existing, err := r.store.FindProposalByHash(originalHash)
		if err == nil && existing != nil {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":      "duplicate_image",
				"message":    "This image was already uploaded",
				"existingId": fmt.Sprintf("%d", existing.ProposalID),
			})
			return
		}
	}

	// Resize for LLM
	llmData := resizeImageForLLM(photoData)

	// Create proposal immediately with "uploaded" status. The OCR stage
	// (if configured) will move it to "parsed_ocr", then the LLM extraction
	// stage to "parsed_llm", and finally to "pending" when ready for review.
	proposal := &domain.Proposal{
		ProposalID:   r.store.ProposalID.Gen(),
		OwnerID:      userID,
		Status:       "uploaded",
		OriginalHash: originalHash,
	}

	// Save photo if photo store is configured
	if r.photoStore != nil {
		photoURL, err := r.photoStore.Save(req.Context(), proposal.ProposalID, photoData)
		if err == nil {
			proposal.PhotoURL = photoURL
		}
	}

	if err := r.store.CreateProposal(proposal); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create proposal")
		return
	}

	// Spawn background parse goroutine with detached context
	go r.parser.ParseReceiptAsync(context.Background(), proposal.ProposalID, llmData, userID)

	writeJSON(w, http.StatusOK, map[string]string{"id": fmt.Sprintf("%d", proposal.ProposalID)})
}

const maxLLMImageDim = 2000

// resizeImageForLLM resizes an image to max 1024px on the longest side
// and re-encodes as JPEG at 80% quality. Returns original if smaller or on error.
func resizeImageForLLM(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("WARNING: could not decode image for resize: %v", err)
		return data
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxLLMImageDim && h <= maxLLMImageDim {
		return data
	}

	// Scale down preserving aspect ratio
	ratio := float64(maxLLMImageDim) / float64(max(w, h))
	newW := int(float64(w) * ratio)
	newH := int(float64(h) * ratio)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		log.Printf("WARNING: could not encode resized image: %v", err)
		return data
	}

	log.Printf("Resized image: %dx%d -> %dx%d (%d -> %d bytes)", w, h, newW, newH, len(data), buf.Len())
	return buf.Bytes()
}
