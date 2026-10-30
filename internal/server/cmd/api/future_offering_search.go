// Package main exposes counted public search through the approved offering DTO.
// focodebase/fobackend/internal/server/cmd/api/future_offering_search.go
package main

import (
	"errors"
	"net/http"
	"strconv"
	"github.com/PiccoloMondoC/focodebase/fobackend/internal/services"
	"github.com/google/uuid"
)

func(app *Application)SearchPublicOfferingsHandler(w http.ResponseWriter,r *http.Request){
	w.Header().Set("Cache-Control","no-store")
	values:=r.URL.Query()
	for _,key:=range []string{"q","sort","page","category_id"}{if len(values[key])>1{app.respondWithErrorCode(w,400,"invalid_search","Supply one value per search setting.");return}}
	input:=services.PublicOfferingSearchRequest{Query:values.Get("q"),Sort:values.Get("sort"),Page:1}
	if raw:=values.Get("page");raw!=""{page,err:=strconv.Atoi(raw);if err!=nil||page<1||page>10000{app.respondWithErrorCode(w,400,"invalid_search","Select a valid results page.");return};input.Page=page}
	if raw:=values.Get("category_id");raw!=""{id,err:=uuid.Parse(raw);if err!=nil||id==uuid.Nil{app.respondWithErrorCode(w,400,"invalid_category","Select a valid category.");return};input.Category=&id}
	result,err:=app.InternalServices.SearchPublicOfferings(r.Context(),input)
	if errors.Is(err,services.ErrInvalidPublicOfferingSearch){app.respondWithErrorCode(w,400,"invalid_search","Use up to 200 characters and 12 search words, and a supported sort order.");return}
	if err!=nil{app.offeringFailure(w,r,err);return}
	items:=make([]OfferingPublicResponse,0,len(result.Items));for _,item:=range result.Items{items=append(items,offeringPublicResponse(item))}
	app.respondWithJSON(w,200,jsonResponse{Message:"Search results",Data:struct{
		Query string `json:"query"`;Total int64 `json:"total"`;Page int `json:"page"`;PageSize int `json:"page_size"`;Items []OfferingPublicResponse `json:"items"`
	}{result.Query,result.Total,result.Page,result.PageSize,items}})
}
