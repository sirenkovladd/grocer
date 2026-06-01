package api

import (
	"net/http"
	"strconv"
	"time"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleAnalysisSpending(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	granularity := req.URL.Query().Get("granularity")

	if granularity == "" {
		granularity = "month"
	}

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter by date range
	var filtered []*domain.Receipt
	for _, receipt := range receipts {
		receiptDate := time.Unix(receipt.Date, 0)
		if from != "" {
			fromDate, err := time.Parse("2006-01-02", from)
			if err == nil && receiptDate.Before(fromDate) {
				continue
			}
		}
		if to != "" {
			toDate, err := time.Parse("2006-01-02", to)
			if err == nil && receiptDate.After(toDate) {
				continue
			}
		}
		filtered = append(filtered, receipt)
	}

	// Group by granularity
	type SpendingPeriod struct {
		Period string  `json:"period"`
		Total  float64 `json:"total"`
	}

	periodMap := make(map[string]float64)
	for _, receipt := range filtered {
		date := time.Unix(receipt.Date, 0)
		var period string
		switch granularity {
		case "day":
			period = date.Format("2006-01-02")
		case "week":
			year, week := date.ISOWeek()
			period = strconv.Itoa(year) + "-W" + strconv.Itoa(week)
		case "month":
			period = date.Format("2006-01")
		}
		periodMap[period] += float64(receipt.TotalCents) / 100.0
	}

	var result []SpendingPeriod
	for period, total := range periodMap {
		result = append(result, SpendingPeriod{Period: period, Total: total})
	}

	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleAnalysisCategories(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")
	owner := req.URL.Query().Get("owner")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter
	var filtered []*domain.Receipt
	for _, receipt := range receipts {
		receiptDate := time.Unix(receipt.Date, 0)
		if from != "" {
			fromDate, err := time.Parse("2006-01-02", from)
			if err == nil && receiptDate.Before(fromDate) {
				continue
			}
		}
		if to != "" {
			toDate, err := time.Parse("2006-01-02", to)
			if err == nil && receiptDate.After(toDate) {
				continue
			}
		}
		if owner != "" {
			ownerID, err := strconv.ParseUint(owner, 10, 64)
			if err == nil && receipt.OwnerID != ownerID {
				continue
			}
		}
		filtered = append(filtered, receipt)
	}

	// Aggregate by category
	type CategoryTotal struct {
		CategoryID uint64  `json:"categoryId"`
		Name       string  `json:"name"`
		Total      float64 `json:"total"`
	}

	categoryMap := make(map[uint64]float64)
	for _, receipt := range filtered {
		for _, item := range receipt.Items {
			itemObj, err := r.store.GetItem(item.ItemID)
			if err != nil {
				continue
			}
			categoryMap[itemObj.CategoryID] += float64(item.Quantity) * float64(item.UnitPriceCents) / 100.0
		}
	}

	var result []CategoryTotal
	for catID, total := range categoryMap {
		cat, err := r.store.GetCategory(catID)
		name := "Unknown"
		if err == nil {
			name = cat.Name
		}
		result = append(result, CategoryTotal{CategoryID: catID, Name: name, Total: total})
	}

	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleAnalysisFamily(w http.ResponseWriter, req *http.Request) {
	from := req.URL.Query().Get("from")
	to := req.URL.Query().Get("to")

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Filter
	var filtered []*domain.Receipt
	for _, receipt := range receipts {
		receiptDate := time.Unix(receipt.Date, 0)
		if from != "" {
			fromDate, err := time.Parse("2006-01-02", from)
			if err == nil && receiptDate.Before(fromDate) {
				continue
			}
		}
		if to != "" {
			toDate, err := time.Parse("2006-01-02", to)
			if err == nil && receiptDate.After(toDate) {
				continue
			}
		}
		filtered = append(filtered, receipt)
	}

	// Aggregate by owner
	type FamilyMember struct {
		UserID uint64  `json:"userId"`
		Name   string  `json:"name"`
		Total  float64 `json:"total"`
	}

	memberMap := make(map[uint64]float64)
	for _, receipt := range filtered {
		memberMap[receipt.OwnerID] += float64(receipt.TotalCents) / 100.0
	}

	var result []FamilyMember
	for userID, total := range memberMap {
		user, err := r.store.GetUserByUserID(userID)
		name := "Unknown"
		if err == nil {
			name = user.Name
		}
		result = append(result, FamilyMember{UserID: userID, Name: name, Total: total})
	}

	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleAnalysisItem(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	itemID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item ID")
		return
	}

	receipts, err := r.store.ListReceipts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Find all receipts containing this item
	type PricePoint struct {
		Date  string  `json:"date"`
		Price float64 `json:"price"`
	}

	var history []PricePoint
	for _, receipt := range receipts {
		for _, item := range receipt.Items {
			if item.ItemID == itemID {
				date := time.Unix(receipt.Date, 0)
				history = append(history, PricePoint{
					Date:  date.Format("2006-01-02"),
					Price: float64(item.UnitPriceCents) / 100.0,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, history)
}
