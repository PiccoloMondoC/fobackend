// Package services tests public search input bounds and literal query handling.
// focodebase/fobackend/internal/services/future_offering_search_test.go
package services

import("errors";"strings";"testing")

func TestPublicOfferingSearchNormalization(t *testing.T){
	q,err:=normalizeOfferingSearch(PublicOfferingSearchRequest{Query:"  AI   assistant %_  "})
	if err!=nil||q.Query!="AI assistant %_"||q.Page!=1||q.Sort!="relevance"{t.Fatal("normalization changed literal terms or defaults")}
	for _,q:=range []PublicOfferingSearchRequest{{Query:strings.Repeat("x",201)},{Query:"bad\x00text"},{Query:strings.Repeat("a ",13)},{Sort:"unknown"},{Page:-1},{Page:10001}}{
		if _,err:=normalizeOfferingSearch(q);!errors.Is(err,ErrInvalidPublicOfferingSearch){t.Fatal("invalid search accepted")}
	}
}
