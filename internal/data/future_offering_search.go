// Package data retrieves counted search pages from approved public facts only.
// focodebase/fobackend/internal/data/future_offering_search.go
package data

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const PublicOfferingSearchPageSize=16
type PublicOfferingSearch struct {
	Terms []string
	Category *uuid.UUID
	Sort string
	Page int
}

// SearchPublicOfferingIDsTx uses one snapshot for count and ordered IDs. All
// terms must occur in public title/summary/description/category/brand text.
// Inline rich-text runs are joined without spaces within their own paragraph or
// list item, so formatting cannot split a searchable word. Only actual text is
// extracted; JSON keys, formatting marks and other metadata are not searchable.
// Literal substring matching
// avoids treating user-entered %, _ or query operators as executable syntax.
// This initial search deliberately excludes private project names, contacts,
// milestones, audit facts and merchant-only records.
func(m *MerchantFutureOfferingModel)SearchPublicOfferingIDsTx(ctx context.Context,tx pgx.Tx,q PublicOfferingSearch)([]uuid.UUID,int64,error){
	ids:=[]uuid.UUID{};var total int64
	if q.Page<1||q.Page>10000||(q.Sort!="relevance"&&q.Sort!="newest"){return nil,0,ErrMerchantFutureOfferingInvalidInput}
	err:=tx.QueryRow(ctx,`WITH matches AS MATERIALIZED (
 SELECT f.id,f.published_at,COALESCE((SELECT sum(
  CASE WHEN strpos(lower(COALESCE(f.title,'')),lower(t.term))>0 THEN 8 ELSE 0 END+
  CASE WHEN strpos(lower(m.name),lower(t.term))>0 THEN 4 ELSE 0 END+
  CASE WHEN strpos(lower(f.summary),lower(t.term))>0 THEN 2 ELSE 0 END+
  CASE WHEN strpos(lower(COALESCE(c.name,'')),lower(t.term))>0 THEN 2 ELSE 0 END+
  CASE WHEN strpos(lower(prose.body),lower(t.term))>0 THEN 1 ELSE 0 END
 ) FROM unnest($1::text[]) AS t(term)),0) AS score
 FROM merchant_future_offerings f
 JOIN future_offering_publication p ON p.future_offering_id=f.id
 JOIN merchants m ON m.id=f.merchant_id AND m.deleted_at IS NULL
 JOIN merchant_accounts a ON a.merchant_id=m.id AND a.account_status='active' AND a.deleted_at IS NULL
 JOIN users u ON u.id=a.principal_user_id AND u.is_active AND u.deleted_at IS NULL
 LEFT JOIN categories c ON c.id=f.category_id AND c.deleted_at IS NULL
 CROSS JOIN LATERAL (
  SELECT COALESCE(string_agg(line.text,' '),'') AS body FROM (
   SELECT (SELECT string_agg(run.value->>'text','' ORDER BY run.ordinal)
     FROM jsonb_array_elements(inlines.value) WITH ORDINALITY AS run(value,ordinal)) AS text
   FROM (
    SELECT value FROM jsonb_path_query(COALESCE(f.description,'{}'::jsonb),'$.blocks[*].content') AS paragraph(value)
    UNION ALL
    SELECT value FROM jsonb_path_query(COALESCE(f.description,'{}'::jsonb),'$.blocks[*].items[*]') AS list_item(value)
   ) AS inlines
  ) AS line
 ) AS prose
 WHERE f.status='published' AND f.deleted_at IS NULL AND p.closed_at IS NULL
 AND ($2::uuid IS NULL OR f.category_id=$2)
 AND NOT EXISTS(SELECT 1 FROM unnest($1::text[]) AS t(term)
   WHERE strpos(lower(concat_ws(' ',f.title,f.summary,m.name,c.name,prose.body)),lower(t.term))=0)
), selected AS (
 SELECT id,published_at,CASE WHEN $3='relevance' THEN score ELSE 0 END AS rank
 FROM matches ORDER BY rank DESC,published_at DESC,id DESC LIMIT 16 OFFSET $4
)
SELECT (SELECT count(*) FROM matches),
 COALESCE((SELECT array_agg(id ORDER BY rank DESC,published_at DESC,id DESC) FROM selected),ARRAY[]::uuid[])`,q.Terms,q.Category,q.Sort,(q.Page-1)*PublicOfferingSearchPageSize).Scan(&total,&ids)
	return ids,total,err
}
