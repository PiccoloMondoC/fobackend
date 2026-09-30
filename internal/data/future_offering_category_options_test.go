// focodebase/fobackend/internal/data/future_offering_category_options_test.go
package data

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFutureOfferingCategoryOptionsAreLeavesWithAncestry(t *testing.T) {
	electronics := &Department{ID: uuid.New(), Name: "Electronics", SortOrder: 10}
	fashion := &Department{ID: uuid.New(), Name: "Fashion", SortOrder: 30}

	consumerTech := &Category{ID: uuid.New(), Name: "Consumer Technology", DepartmentID: electronics.ID, SortOrder: 150}
	wearables := &Category{ID: uuid.New(), Name: "Wearable Technology", DepartmentID: electronics.ID, ParentID: &consumerTech.ID, SortOrder: 100}
	men := &Category{ID: uuid.New(), Name: "Men", DepartmentID: fashion.ID, SortOrder: 110}
	women := &Category{ID: uuid.New(), Name: "Women", DepartmentID: fashion.ID, SortOrder: 100}
	menShoes := &Category{ID: uuid.New(), Name: "Shoes", DepartmentID: fashion.ID, ParentID: &men.ID, SortOrder: 110}
	womenShoes := &Category{ID: uuid.New(), Name: "Shoes", DepartmentID: fashion.ID, ParentID: &women.ID, SortOrder: 110}
	orphanParent := uuid.New() // parent soft-deleted (absent from active rows)
	orphan := &Category{ID: uuid.New(), Name: "Orphan", DepartmentID: fashion.ID, ParentID: &orphanParent}
	noDept := &Category{ID: uuid.New(), Name: "No department", DepartmentID: uuid.New()}

	options := BuildFutureOfferingCategoryOptions(
		[]*Department{fashion, electronics},
		[]*Category{menShoes, womenShoes, wearables, consumerTech, men, women, orphan, noDept},
	)

	var got []string
	for _, o := range options {
		got = append(got, o.DepartmentName+" > "+strings.Join(o.Path, " > "))
	}
	want := []string{
		"Electronics > Consumer Technology > Wearable Technology",
		"Fashion > Women > Shoes",
		"Fashion > Men > Shoes",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if options[0].ID != wearables.ID || options[0].Name != "Wearable Technology" {
		t.Fatalf("option identity must be the leaf category")
	}
}
