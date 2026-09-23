// Package main provides HTTP handlers for the Platform API.
//
// focodebase/fobackend/internal/server/cmd/api/departments.go
//
// GTM:
//
//	Layer: 2.5 Catalog / Offer Domain
//	Release Class: SPINE
//	Reason:
//	  Department-rooted category administration is release-critical catalog
//	  taxonomy infrastructure. These handlers preserve department-first
//	  classification, category hierarchy, slug-based identity, stable taxonomy
//	  ordering, and soft-delete lifecycle behavior required by Future Offering
//	  publication and public catalog navigation.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Preserve authorization enforcement.
//	Preserve trusted-context actor and category ID extraction.
//	Preserve department-first taxonomy behavior.
//	Preserve category hierarchy and parent-child lookup semantics.
//	Preserve slug-based identity and lookup compatibility.
//	Preserve stable taxonomy ordering.
//	Preserve DB-owned lifecycle timestamps.
//	Preserve canonical soft-delete behavior.
//	Preserve hard delete as a non-routed physical purge path.
//	Preserve best-effort audit logging.
//	Block deployment if this file breaks build, taxonomy persistence,
//	offer classification, department-first routing, hierarchy integrity,
//	or category lifecycle behavior.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"

	"github.com/google/uuid"
)

const (
	categoryEntityTypeName        = "category"
	categoryEntityTypeDescription = "Department-rooted catalog category entity"

	createCategoryAction     = "create_category"
	readCategoryAction       = "read_category"
	listCategoriesAction     = "list_categories"
	updateCategoryAction     = "update_category"
	softDeleteCategoryAction = "soft_delete_category"
)

type createCategoryInput struct {
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	DepartmentID string  `json:"department_id"`
	ParentID     *string `json:"parent_id,omitempty"`
	SortOrder    int     `json:"sort_order"`
	Description  *string `json:"description,omitempty"`
}

type updateCategoryInput struct {
	Name           *string
	Slug           *string
	DepartmentID   *string
	ParentID       *string
	SortOrder      *int
	Description    *string
	DescriptionSet bool
}

// CreateCategoryHandler creates a category within the department-rooted
// taxonomy.
//
// Department association, slug identity, hierarchy, and presentation order are
// required parts of the category persistence contract. Lifecycle timestamps are
// owned by the database.
func (app *Application) CreateCategoryHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("CreateCategoryHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, createCategoryAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	var input createCategoryInput
	if err := app.readJSON(w, r, &input); err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		app.respondWithError(
			w,
			errors.New("name is required"),
			http.StatusBadRequest,
		)
		return
	}

	slug := normalizeCategorySlug(input.Slug)
	if slug == "" {
		app.respondWithError(
			w,
			errors.New("slug is required"),
			http.StatusBadRequest,
		)
		return
	}

	departmentID, err := parseRequiredCategoryUUID(
		input.DepartmentID,
		"department_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if _, err := app.Models.Department.GetByID(ctx, departmentID); err != nil {
		if errors.Is(err, data.ErrDepartmentNotFound) {
			app.respondWithError(
				w,
				errors.New("department not found"),
				http.StatusBadRequest,
			)
			return
		}

		logger.Error(
			"Validate category department failed",
			"department_id", departmentID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to validate department: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	parentID, err := parseOptionalCategoryUUID(
		input.ParentID,
		"parent_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	if parentID != nil {
		parent, err := app.Models.Category.GetByID(ctx, *parentID)
		if err != nil {
			if errors.Is(err, data.ErrCategoryNotFound) {
				app.respondWithError(
					w,
					errors.New("parent category not found"),
					http.StatusBadRequest,
				)
				return
			}

			logger.Error(
				"Validate parent category failed",
				"parent_id", *parentID,
				"error", err,
			)
			app.respondWithError(
				w,
				fmt.Errorf("failed to validate parent category: %w", err),
				http.StatusInternalServerError,
			)
			return
		}

		if parent.DepartmentID != departmentID {
			app.respondWithError(
				w,
				errors.New(
					"parent category must belong to the same department",
				),
				http.StatusBadRequest,
			)
			return
		}
	}

	category := &data.Category{
		ID:           uuid.New(),
		Name:         name,
		Slug:         slug,
		DepartmentID: departmentID,
		ParentID:     parentID,
		SortOrder:    input.SortOrder,
		Description:  normalizeCategoryOptionalText(input.Description),
	}

	if err := app.Models.Category.Insert(ctx, category); err != nil {
		logger.Error(
			"Create category failed",
			"department_id", departmentID,
			"slug", slug,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to create category: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertCategoryAudit(
		ctx,
		userID,
		createCategoryAction,
		"Create a department-rooted catalog category",
		category.ID.String(),
	)

	logger.Info(
		"Category created",
		"category_id", category.ID,
		"department_id", category.DepartmentID,
		"parent_id", category.ParentID,
		"slug", category.Slug,
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusCreated, jsonResponse{
		Error:   false,
		Message: "Category created successfully",
		Data:    category,
	})
}

// GetCategoryByIDHandler retrieves one active category using the trusted
// category ID installed in request context by route middleware.
func (app *Application) GetCategoryByIDHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetCategoryByIDHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, readCategoryAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	categoryID := app.getCategoryIDFromContext(ctx)
	if categoryID == nil || *categoryID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("category ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	category, err := app.Models.Category.GetByID(ctx, *categoryID)
	if err != nil {
		if errors.Is(err, data.ErrCategoryNotFound) {
			app.respondWithError(
				w,
				errors.New("category not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve category failed",
			"category_id", *categoryID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve category: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertCategoryAudit(
		ctx,
		userID,
		readCategoryAction,
		"Read a category by ID",
		category.ID.String(),
	)

	logger.Info(
		"Category retrieved",
		"category_id", category.ID,
		"department_id", category.DepartmentID,
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Category retrieved successfully",
		Data:    category,
	})
}

// GetAllCategoriesHandler retrieves categories using the data layer's stable
// taxonomy ordering.
//
// Supported query modes:
//
//	GET ...                                  list all active categories
//	GET ...?department_id=<uuid>             list a department's categories
//	GET ...?department_id=<uuid>&roots=true  list department root categories
//	GET ...?parent_id=<uuid>                 list a category's direct children
//
// department_id and parent_id are intentionally mutually exclusive.
func (app *Application) GetAllCategoriesHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("GetAllCategoriesHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, listCategoriesAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	query := r.URL.Query()

	departmentID, err := parseCategoryQueryUUID(
		query.Get("department_id"),
		"department_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	parentID, err := parseCategoryQueryUUID(
		query.Get("parent_id"),
		"parent_id",
	)
	if err != nil {
		app.respondWithError(w, err, http.StatusBadRequest)
		return
	}

	rootsOnly := false
	if rawRoots := strings.TrimSpace(query.Get("roots")); rawRoots != "" {
		rootsOnly, err = strconv.ParseBool(rawRoots)
		if err != nil {
			app.respondWithError(
				w,
				errors.New("roots must be true or false"),
				http.StatusBadRequest,
			)
			return
		}
	}

	if departmentID != nil && parentID != nil {
		app.respondWithError(
			w,
			errors.New(
				"department_id and parent_id cannot be used together",
			),
			http.StatusBadRequest,
		)
		return
	}

	if rootsOnly && departmentID == nil {
		app.respondWithError(
			w,
			errors.New("department_id is required when roots is true"),
			http.StatusBadRequest,
		)
		return
	}

	if rootsOnly && parentID != nil {
		app.respondWithError(
			w,
			errors.New("roots and parent_id cannot be used together"),
			http.StatusBadRequest,
		)
		return
	}

	var categories []*data.Category

	switch {
	case parentID != nil:
		categories, err = app.Models.Category.GetByParentID(ctx, *parentID)

	case rootsOnly:
		categories, err = app.Models.Category.GetRootsByDepartmentID(
			ctx,
			*departmentID,
		)

	case departmentID != nil:
		categories, err = app.Models.Category.GetByDepartmentID(
			ctx,
			*departmentID,
		)

	default:
		categories, err = app.Models.Category.GetAll(ctx)
	}

	if err != nil {
		logger.Error(
			"List categories failed",
			"department_id", departmentID,
			"parent_id", parentID,
			"roots", rootsOnly,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve categories: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertCategoryAudit(
		ctx,
		userID,
		listCategoriesAction,
		"List catalog categories",
		"",
	)

	logger.Info(
		"Categories retrieved",
		"count", len(categories),
		"department_id", departmentID,
		"parent_id", parentID,
		"roots", rootsOnly,
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Categories retrieved successfully",
		Data:    categories,
	})
}

// UpdateCategoryHandler updates an active category.
//
// CategoryModel.Update replaces every mutable persistence field. The current row
// is therefore loaded first and only supplied request fields are applied. This
// prevents omitted JSON fields from erasing department, hierarchy, slug,
// ordering, or descriptive state.
func (app *Application) UpdateCategoryHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("UpdateCategoryHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, updateCategoryAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	categoryID := app.getCategoryIDFromContext(ctx)
	if categoryID == nil || *categoryID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("category ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	category, err := app.Models.Category.GetByID(ctx, *categoryID)
	if err != nil {
		if errors.Is(err, data.ErrCategoryNotFound) {
			app.respondWithError(
				w,
				errors.New("category not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Retrieve category for update failed",
			"category_id", *categoryID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to retrieve category: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	input, err := app.readCategoryUpdateInput(w, r)
	if err != nil {
		app.respondWithError(
			w,
			fmt.Errorf("invalid JSON input: %w", err),
			http.StatusBadRequest,
		)
		return
	}

	if input.Name == nil &&
		input.Slug == nil &&
		input.DepartmentID == nil &&
		input.ParentID == nil &&
		input.SortOrder == nil &&
		!input.DescriptionSet {
		app.respondWithError(
			w,
			errors.New("at least one update field is required"),
			http.StatusBadRequest,
		)
		return
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			app.respondWithError(
				w,
				errors.New("name cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}

		category.Name = name
	}

	if input.Slug != nil {
		slug := normalizeCategorySlug(*input.Slug)
		if slug == "" {
			app.respondWithError(
				w,
				errors.New("slug cannot be empty"),
				http.StatusBadRequest,
			)
			return
		}

		category.Slug = slug
	}

	if input.DepartmentID != nil {
		departmentID, err := parseRequiredCategoryUUID(
			*input.DepartmentID,
			"department_id",
		)
		if err != nil {
			app.respondWithError(w, err, http.StatusBadRequest)
			return
		}

		if _, err := app.Models.Department.GetByID(ctx, departmentID); err != nil {
			if errors.Is(err, data.ErrDepartmentNotFound) {
				app.respondWithError(
					w,
					errors.New("department not found"),
					http.StatusBadRequest,
				)
				return
			}

			logger.Error(
				"Validate updated category department failed",
				"department_id", departmentID,
				"error", err,
			)
			app.respondWithError(
				w,
				fmt.Errorf("failed to validate department: %w", err),
				http.StatusInternalServerError,
			)
			return
		}

		category.DepartmentID = departmentID
	}

	if input.ParentID != nil {
		parentID, err := parseNullableCategoryUUID(
			*input.ParentID,
			"parent_id",
		)
		if err != nil {
			app.respondWithError(w, err, http.StatusBadRequest)
			return
		}

		category.ParentID = parentID
	}

	if input.SortOrder != nil {
		category.SortOrder = *input.SortOrder
	}

	if input.DescriptionSet {
		category.Description = normalizeCategoryOptionalText(
			input.Description,
		)
	}

	if category.ParentID != nil {
		if *category.ParentID == category.ID {
			app.respondWithError(
				w,
				errors.New("a category cannot be its own parent"),
				http.StatusBadRequest,
			)
			return
		}

		parent, err := app.Models.Category.GetByID(ctx, *category.ParentID)
		if err != nil {
			if errors.Is(err, data.ErrCategoryNotFound) {
				app.respondWithError(
					w,
					errors.New("parent category not found"),
					http.StatusBadRequest,
				)
				return
			}

			logger.Error(
				"Validate updated parent category failed",
				"parent_id", *category.ParentID,
				"error", err,
			)
			app.respondWithError(
				w,
				fmt.Errorf("failed to validate parent category: %w", err),
				http.StatusInternalServerError,
			)
			return
		}

		if parent.DepartmentID != category.DepartmentID {
			app.respondWithError(
				w,
				errors.New(
					"parent category must belong to the same department",
				),
				http.StatusBadRequest,
			)
			return
		}
	}

	if err := app.Models.Category.Update(ctx, category); err != nil {
		if errors.Is(err, data.ErrCategoryNotFound) {
			app.respondWithError(
				w,
				errors.New("category not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Update category failed",
			"category_id", category.ID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to update category: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertCategoryAudit(
		ctx,
		userID,
		updateCategoryAction,
		"Update a department-rooted catalog category",
		category.ID.String(),
	)

	logger.Info(
		"Category updated",
		"category_id", category.ID,
		"department_id", category.DepartmentID,
		"parent_id", category.ParentID,
		"slug", category.Slug,
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Category updated successfully",
		Data:    category,
	})
}

// DeleteCategoryHandler performs the canonical category soft-delete lifecycle
// operation.
//
// CategoryModel.Delete is the physical purge path and is intentionally not
// exposed by this handler.
func (app *Application) DeleteCategoryHandler(
	w http.ResponseWriter,
	r *http.Request,
) {
	logger := app.Logger.
		GetLoggerWithContext(r).
		WithFunctionName("DeleteCategoryHandler")

	ctx, cancel := context.WithTimeout(r.Context(), cfgTimeout)
	defer cancel()

	if !app.HasPermission(ctx, softDeleteCategoryAction) {
		app.respondWithError(
			w,
			errors.New("forbidden: insufficient permissions"),
			http.StatusForbidden,
		)
		return
	}

	userID := app.getUserIDFromContext(ctx)
	if userID == nil || *userID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("user ID not found in context"),
			http.StatusUnauthorized,
		)
		return
	}

	categoryID := app.getCategoryIDFromContext(ctx)
	if categoryID == nil || *categoryID == uuid.Nil {
		app.respondWithError(
			w,
			errors.New("category ID not found in context"),
			http.StatusBadRequest,
		)
		return
	}

	if err := app.Models.Category.SoftDelete(ctx, *categoryID); err != nil {
		if errors.Is(err, data.ErrCategoryNotFound) {
			app.respondWithError(
				w,
				errors.New("category not found"),
				http.StatusNotFound,
			)
			return
		}

		logger.Error(
			"Soft delete category failed",
			"category_id", *categoryID,
			"error", err,
		)
		app.respondWithError(
			w,
			fmt.Errorf("failed to soft-delete category: %w", err),
			http.StatusInternalServerError,
		)
		return
	}

	app.insertCategoryAudit(
		ctx,
		userID,
		softDeleteCategoryAction,
		"Soft-delete a catalog category",
		categoryID.String(),
	)

	logger.Info(
		"Category soft-deleted",
		"category_id", *categoryID,
		"user_id", *userID,
	)

	app.respondWithJSON(w, http.StatusOK, jsonResponse{
		Error:   false,
		Message: "Category deleted successfully",
		Data:    *categoryID,
	})
}

// insertCategoryAudit performs best-effort category audit persistence.
//
// Audit metadata or audit insertion failure must not reverse or misrepresent a
// completed SPINE taxonomy operation.
func (app *Application) insertCategoryAudit(
	ctx context.Context,
	userID *uuid.UUID,
	actionName string,
	actionDescription string,
	entityID string,
) {
	if userID == nil || *userID == uuid.Nil {
		return
	}

	action, err := app.Models.Action.GetByName(ctx, actionName)
	if err != nil || action == nil {
		actionID, createErr := app.Models.Action.CreateIfNotExists(
			ctx,
			actionName,
			actionDescription,
		)
		if createErr != nil {
			return
		}

		action = &data.Action{
			ID: actionID,
		}
	}

	entityType, err := app.Models.EntityType.GetByName(
		ctx,
		categoryEntityTypeName,
	)
	if err != nil || entityType == nil {
		entityTypeID, createErr := app.Models.EntityType.CreateIfNotExists(
			ctx,
			categoryEntityTypeName,
			categoryEntityTypeDescription,
		)
		if createErr != nil {
			return
		}

		entityType = &data.EntityType{
			ID: entityTypeID,
		}
	}

	if action == nil || action.ID == uuid.Nil {
		return
	}

	if entityType == nil || entityType.ID == uuid.Nil {
		return
	}

	auditLog := &data.AuditLog{
		ID:           uuid.New(),
		UserID:       userID,
		ActionID:     action.ID,
		EntityTypeID: entityType.ID,
		EntityID:     entityID,
	}

	_ = app.Models.AuditLog.Insert(ctx, auditLog)
}

// readCategoryUpdateInput reads a partial category update while preserving
// whether nullable fields were explicitly included in the JSON document.
func (app *Application) readCategoryUpdateInput(
	w http.ResponseWriter,
	r *http.Request,
) (*updateCategoryInput, error) {
	var raw map[string]any
	if err := app.readJSON(w, r, &raw); err != nil {
		return nil, err
	}

	input := &updateCategoryInput{}

	if value, exists := raw["name"]; exists {
		if value == nil {
			return nil, errors.New("name cannot be null")
		}

		name, ok := value.(string)
		if !ok {
			return nil, errors.New("name must be a string")
		}

		input.Name = &name
	}

	if value, exists := raw["slug"]; exists {
		if value == nil {
			return nil, errors.New("slug cannot be null")
		}

		slug, ok := value.(string)
		if !ok {
			return nil, errors.New("slug must be a string")
		}

		input.Slug = &slug
	}

	if value, exists := raw["department_id"]; exists {
		if value == nil {
			return nil, errors.New("department_id cannot be null")
		}

		departmentID, ok := value.(string)
		if !ok {
			return nil, errors.New("department_id must be a string")
		}

		input.DepartmentID = &departmentID
	}

	if value, exists := raw["parent_id"]; exists {
		if value == nil {
			emptyParentID := ""
			input.ParentID = &emptyParentID
		} else {
			parentID, ok := value.(string)
			if !ok {
				return nil, errors.New(
					"parent_id must be a UUID string or null",
				)
			}

			input.ParentID = &parentID
		}
	}

	if value, exists := raw["sort_order"]; exists {
		if value == nil {
			return nil, errors.New("sort_order cannot be null")
		}

		number, ok := value.(float64)
		if !ok {
			return nil, errors.New("sort_order must be an integer")
		}

		sortOrder := int(number)
		if number != float64(sortOrder) {
			return nil, errors.New("sort_order must be an integer")
		}

		input.SortOrder = &sortOrder
	}

	if value, exists := raw["description"]; exists {
		input.DescriptionSet = true

		if value == nil {
			input.Description = nil
		} else {
			description, ok := value.(string)
			if !ok {
				return nil, errors.New(
					"description must be a string or null",
				)
			}

			input.Description = &description
		}
	}

	return input, nil
}

func parseRequiredCategoryUUID(
	rawValue string,
	fieldName string,
) (uuid.UUID, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return uuid.Nil, fmt.Errorf("%s is required", fieldName)
	}

	id, err := uuid.Parse(rawValue)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid %s", fieldName)
	}

	return id, nil
}

func parseOptionalCategoryUUID(
	rawValue *string,
	fieldName string,
) (*uuid.UUID, error) {
	if rawValue == nil {
		return nil, nil
	}

	return parseNullableCategoryUUID(*rawValue, fieldName)
}

func parseNullableCategoryUUID(
	rawValue string,
	fieldName string,
) (*uuid.UUID, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return nil, nil
	}

	id, err := uuid.Parse(rawValue)
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("invalid %s", fieldName)
	}

	return &id, nil
}

func parseCategoryQueryUUID(
	rawValue string,
	fieldName string,
) (*uuid.UUID, error) {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return nil, nil
	}

	id, err := uuid.Parse(rawValue)
	if err != nil || id == uuid.Nil {
		return nil, fmt.Errorf("invalid %s", fieldName)
	}

	return &id, nil
}

func normalizeCategorySlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeCategoryOptionalText(value *string) *string {
	if value == nil {
		return nil
	}

	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}

	return &normalized
}
