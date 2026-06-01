package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"code.sirenko.ca/grocer/internal/domain"
)

func (r *Router) handleListCategories(w http.ResponseWriter, req *http.Request) {
	categories, err := r.store.ListCategories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

type createCategoryRequest struct {
	Name     string  `json:"name"`
	ParentID *uint64 `json:"parentId,omitempty"`
}

func (r *Router) handleCreateCategory(w http.ResponseWriter, req *http.Request) {
	var reqBody createCategoryRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Input validation
	if err := validateCategoryName(reqBody.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	category := &domain.Category{
		CategoryID: r.store.CategoryID.Gen(),
		Name:       reqBody.Name,
		ParentID:   reqBody.ParentID,
	}

	if err := r.store.CreateCategory(category); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, category)
}

type updateCategoryRequest struct {
	Name      *string `json:"name,omitempty"`
	ParentID  *uint64 `json:"parentId,omitempty"`
	SortOrder *int32  `json:"sortOrder,omitempty"`
}

func (r *Router) handleUpdateCategory(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	category, err := r.store.GetCategory(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "category not found")
		return
	}

	var reqBody updateCategoryRequest
	if err := json.NewDecoder(req.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if reqBody.Name != nil {
		if err := validateCategoryName(*reqBody.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		category.Name = *reqBody.Name
	}
	if reqBody.ParentID != nil {
		category.ParentID = reqBody.ParentID
	}
	if reqBody.SortOrder != nil {
		category.SortOrder = *reqBody.SortOrder
	}

	if err := r.store.UpdateCategory(category); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, category)
}

func (r *Router) handleDeleteCategory(w http.ResponseWriter, req *http.Request) {
	idStr := req.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid category ID")
		return
	}

	// Check if any items are using this category
	items, err := r.store.ListItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	for _, item := range items {
		if item.CategoryID == id {
			writeError(w, http.StatusBadRequest, "cannot delete category: items are using this category")
			return
		}
	}

	if err := r.store.DeleteCategory(id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
