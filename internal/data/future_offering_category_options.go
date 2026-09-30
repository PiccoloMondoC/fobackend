// Package data provides the Future Offering view of the category taxonomy.
//
// focodebase/fobackend/internal/data/future_offering_category_options.go
//
// GTM:
//
//	Layer: 2.5 Minimal Catalog / Future Offering Classification
//	Release Class: SPINE
//	Reason:
//	  PCDF-M01 asks the merchant for ONE category that best defines the
//	  product or service. Broader classification comes from that category's
//	  ancestry (department and parent categories), and the most specific
//	  category may sit at any depth. The department/category architecture is
//	  unchanged; this file only derives what M01 needs from it:
//
//	    * selectable categories = active leaves (no active children) whose
//	      department and every ancestor are active;
//	    * each option carries its full ancestry path so identically named
//	      leaves ("Shoes" under Women, Men, Kids) are distinguishable.
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Keep BuildFutureOfferingCategoryOptions pure (tested without a database).
//	Keep IsSelectableForFutureOfferingTx consistent with the options builder.
package data

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// FutureOfferingCategoryOption is one category a merchant may choose.
type FutureOfferingCategoryOption struct {
	ID uuid.UUID `json:"id"`
	// Name is the category's own name.
	Name string `json:"name"`
	// DepartmentName is the top of the ancestry.
	DepartmentName string `json:"department_name"`
	// Path is the ancestry below the department, ending with Name.
	Path []string `json:"path"`

	sortKey []int
}

// BuildFutureOfferingCategoryOptions derives selectable options from active
// departments and active categories. Inputs are expected to exclude
// soft-deleted rows (DepartmentModel.GetAll / CategoryModel.GetAll do).
func BuildFutureOfferingCategoryOptions(departments []*Department, categories []*Category) []FutureOfferingCategoryOption {
	deptByID := make(map[uuid.UUID]*Department, len(departments))
	for _, d := range departments {
		deptByID[d.ID] = d
	}
	catByID := make(map[uuid.UUID]*Category, len(categories))
	hasChild := make(map[uuid.UUID]bool, len(categories))
	for _, c := range categories {
		catByID[c.ID] = c
	}
	for _, c := range categories {
		if c.ParentID != nil {
			hasChild[*c.ParentID] = true
		}
	}

	options := make([]FutureOfferingCategoryOption, 0)
	for _, c := range categories {
		if hasChild[c.ID] {
			continue
		}
		dept, ok := deptByID[c.DepartmentID]
		if !ok {
			continue
		}
		path, keys, ok := categoryAncestry(c, catByID)
		if !ok {
			continue
		}
		options = append(options, FutureOfferingCategoryOption{
			ID:             c.ID,
			Name:           c.Name,
			DepartmentName: dept.Name,
			Path:           path,
			sortKey:        append([]int{dept.SortOrder}, keys...),
		})
	}

	sort.SliceStable(options, func(i, j int) bool {
		a, b := options[i], options[j]
		if a.sortKey[0] != b.sortKey[0] {
			return a.sortKey[0] < b.sortKey[0]
		}
		if a.DepartmentName != b.DepartmentName {
			return a.DepartmentName < b.DepartmentName
		}
		// Compare level by level: sort_order first, then name.
		for k := 0; k < len(a.Path) && k < len(b.Path); k++ {
			if a.sortKey[k+1] != b.sortKey[k+1] {
				return a.sortKey[k+1] < b.sortKey[k+1]
			}
			if a.Path[k] != b.Path[k] {
				return a.Path[k] < b.Path[k]
			}
		}
		return len(a.Path) < len(b.Path)
	})
	return options
}

// categoryAncestry walks to the root. It fails when an ancestor is missing
// (deleted), crosses departments, or forms a cycle.
func categoryAncestry(c *Category, byID map[uuid.UUID]*Category) ([]string, []int, bool) {
	var names []string
	var keys []int
	seen := map[uuid.UUID]bool{}
	for cur := c; cur != nil; {
		if seen[cur.ID] {
			return nil, nil, false
		}
		seen[cur.ID] = true
		names = append([]string{cur.Name}, names...)
		keys = append([]int{cur.SortOrder}, keys...)
		if cur.ParentID == nil {
			break
		}
		parent, ok := byID[*cur.ParentID]
		if !ok || parent.DepartmentID != c.DepartmentID {
			return nil, nil, false
		}
		cur = parent
	}
	return names, keys, true
}

// ListFutureOfferingCategoryOptions returns selectable leaf categories.
func (m *CategoryModel) ListFutureOfferingCategoryOptions(ctx context.Context, departments *DepartmentModel) ([]FutureOfferingCategoryOption, error) {
	if m == nil || departments == nil {
		return nil, errors.New("category and department models are required")
	}
	depts, err := departments.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	cats, err := m.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return BuildFutureOfferingCategoryOptions(depts, cats), nil
}

// FutureOfferingCategorySelectability describes a referenced category.
type FutureOfferingCategorySelectability int

// Category selectability states.
const (
	FutureOfferingCategorySelectable FutureOfferingCategorySelectability = iota
	FutureOfferingCategoryUnavailable
	FutureOfferingCategoryNotSpecific
)

// IsSelectableForFutureOfferingTx classifies a category for readiness inside
// the caller's transaction, using the same rules as the options builder.
func (m *CategoryModel) IsSelectableForFutureOfferingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (FutureOfferingCategorySelectability, error) {
	if m == nil || tx == nil || id == uuid.Nil {
		return FutureOfferingCategoryUnavailable, errors.New("category model, transaction and id are required")
	}
	ctx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	// Walk ancestry with a bounded recursive query; any deleted node,
	// deleted department or department mismatch makes the category
	// unavailable.
	const query = `
		WITH RECURSIVE chain AS (
			SELECT c.id, c.parent_id, c.department_id, c.deleted_at, 0 AS depth
			FROM categories c
			WHERE c.id = $1
			UNION ALL
			SELECT p.id, p.parent_id, p.department_id, p.deleted_at, chain.depth + 1
			FROM categories p
			JOIN chain ON p.id = chain.parent_id
			WHERE chain.depth < 32
		)
		SELECT
			EXISTS (SELECT 1 FROM chain WHERE depth = 0),
			NOT EXISTS (
				SELECT 1 FROM chain
				LEFT JOIN departments d ON d.id = chain.department_id
				WHERE chain.deleted_at IS NOT NULL
				   OR d.id IS NULL
				   OR d.deleted_at IS NOT NULL
				   OR chain.department_id <> (SELECT department_id FROM chain WHERE depth = 0)
			),
			EXISTS (
				SELECT 1 FROM categories child
				WHERE child.parent_id = $1 AND child.deleted_at IS NULL
			)
	`
	var exists, intact, hasChildren bool
	if err := tx.QueryRow(ctx, query, id).Scan(&exists, &intact, &hasChildren); err != nil {
		return FutureOfferingCategoryUnavailable, err
	}
	switch {
	case !exists || !intact:
		return FutureOfferingCategoryUnavailable, nil
	case hasChildren:
		return FutureOfferingCategoryNotSpecific, nil
	default:
		return FutureOfferingCategorySelectable, nil
	}
}
