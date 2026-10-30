// Package services composes coherent public offering search without private data.
// focodebase/fobackend/internal/services/future_offering_search.go
package services

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrInvalidPublicOfferingSearch=errors.New("invalid public offering search")
type PublicOfferingSearchRequest struct {Query,Sort string;Category *uuid.UUID;Page int}
type PublicOfferingSearchResult struct {Query string;Total int64;Page,PageSize int;Items []FutureOfferingPreview}

func normalizeOfferingSearch(in PublicOfferingSearchRequest)(PublicOfferingSearchRequest,error){
	if !utf8.ValidString(in.Query)||utf8.RuneCountInString(in.Query)>200||strings.IndexFunc(in.Query,unicode.IsControl)>=0{return in,ErrInvalidPublicOfferingSearch}
	in.Query=strings.Join(strings.Fields(in.Query)," ")
	if len(strings.Fields(in.Query))>12{return in,ErrInvalidPublicOfferingSearch}
	if in.Sort==""{in.Sort="relevance"};if in.Page==0{in.Page=1}
	if (in.Sort!="relevance"&&in.Sort!="newest")||in.Page<1||in.Page>10000||(in.Category!=nil&&*in.Category==uuid.Nil){return in,ErrInvalidPublicOfferingSearch}
	return in,nil
}

// Count, page membership and public projections share a repeatable snapshot.
// PublicOfferingTx still enforces canonical publication eligibility. Concurrent
// publication changes can cause a serialization failure while taking its share
// lock; this read-only operation may safely retry at most twice within context.
func(s *Service)SearchPublicOfferings(ctx context.Context,in PublicOfferingSearchRequest)(PublicOfferingSearchResult,error){
	var result PublicOfferingSearchResult
	if ctx==nil{return result,ErrNilContext}
	ctx,cancel:=context.WithTimeout(ctx,12*time.Second);defer cancel()
	in,err:=normalizeOfferingSearch(in);if err!=nil{return result,err}
	for attempt:=0;attempt<3;attempt++{
		result=PublicOfferingSearchResult{Query:in.Query,Page:in.Page,PageSize:data.PublicOfferingSearchPageSize,Items:[]FutureOfferingPreview{}}
		err=s.runMerchantFutureOfferingTx(ctx,"SearchPublicOfferings",false,func(ctx context.Context,tx pgx.Tx)error{
			if _,err:=tx.Exec(ctx,`SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`);err!=nil{return err}
			if in.Category!=nil{
				choice,err:=s.Models.Category.IsSelectableForFutureOfferingTx(ctx,tx,*in.Category);if err!=nil{return err}
				if choice!=data.FutureOfferingCategorySelectable{return data.ErrMerchantFutureOfferingNotFound}
			}
			terms:=strings.Fields(in.Query);if terms==nil{terms=[]string{}}
			ids,total,err:=s.Models.MerchantFutureOffering.SearchPublicOfferingIDsTx(ctx,tx,data.PublicOfferingSearch{Terms:terms,Category:in.Category,Sort:in.Sort,Page:in.Page});if err!=nil{return err};result.Total=total
			for _,id:=range ids{
				fo,brand,err:=s.Models.MerchantFutureOffering.PublicOfferingTx(ctx,tx,id);if err!=nil{return err}
				media,err:=s.Models.MerchantFutureOffering.ListMediaTx(ctx,tx,id);if err!=nil{return err}
				result.Items=append(result.Items,FutureOfferingPreview{Offering:fo,Brand:brand,Media:media})
			}
			return nil
		})
		var databaseError *pgconn.PgError
		if err==nil||!errors.As(err,&databaseError)||databaseError.Code!="40001"{return result,err}
	}
	return result,err
}
