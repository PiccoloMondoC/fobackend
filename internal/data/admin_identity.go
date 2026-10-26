// Package data provides account identity completion and administrative identity safeguards.
// focodebase/fobackend/internal/data/admin_identity.go

package data

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// AdministrativeIdentity is display data, never an authorization target.
// Complete means the required profile identity is valid; optional details do
// not gate identity presentation. Handle ownership is checked in the canonical
// namespace. Legal names and other private profile values never leave storage.
type AdministrativeIdentity struct {
	UserID          uuid.UUID `json:"user_id"`
	UserHandle      *string   `json:"user_handle"`
	EmailPrefix     string    `json:"email_prefix"`
	ProfileComplete bool      `json:"profile_complete"`
}

func administrativeIdentity(id uuid.UUID, email string, p *UserProfile, privateNamePresent, claimed bool) (*AdministrativeIdentity, error) {
	// A corrupt live or retained account fails the read; do not invent either
	// a handle or an account-without-email presentation.
	at := strings.LastIndexByte(email, '@')
	if id == uuid.Nil || at <= 0 || at == len(email)-1 || strings.Contains(email[:at], "@") || strings.TrimSpace(email[:at]) == "" || !profileClean(email[:at], false) {
		return nil, errors.New("invalid administrative identity")
	}
	identity := &AdministrativeIdentity{UserID: id, EmailPrefix: email[:at]}
	if p == nil || p.DeletedAt != nil || !privateNamePresent || !claimed {
		return identity, nil
	}
	for _, field := range []string{"user_handle"} {
		value := ProfileFieldValue(p, field)
		if value == nil || strings.TrimSpace(*value) == "" || ProfileFieldProblem(field, value) != "" {
			return identity, nil
		}
	}
	identity.ProfileComplete = true
	handle := *p.UserHandle
	identity.UserHandle = &handle
	return identity, nil
}

// ReadAdministrativeIdentities uses one bounded, nonlocking statement. Callers
// must authorize first and close earlier cursors before invoking it. Passing a
// transaction reuses its connection: no pool acquisition or new lock order.
// Private names are validated internally and never enter the returned DTO.
// Only explicit full legal names count; legacy first/last values do not.
func ReadAdministrativeIdentities(ctx context.Context, q profileQueryer, ids ...uuid.UUID) (map[uuid.UUID]*AdministrativeIdentity, error) {
	unique := SortedGovernanceLockOrder(ids...)
	result := make(map[uuid.UUID]*AdministrativeIdentity, len(unique))
	if len(unique) == 0 {
		return result, nil
	}
	if len(unique) > 2000 {
		return nil, errors.New("administrative identity read exceeds bound")
	}
	rows, err := q.Query(ctx, `
		SELECT u.id, u.email::text, p.user_handle::text,
		       p.preferred_contact, p.phone, p.deleted_at,
		       (SELECT pi.full_legal_name FROM user_profile_private_identities pi WHERE pi.user_id=u.id),
		       EXISTS(SELECT 1 FROM global_handles gh
		              WHERE gh.entity_type='user' AND gh.entity_id=u.id AND gh.handle=p.user_handle)
		FROM users u LEFT JOIN user_profiles p ON p.user_id=u.id
		WHERE u.id=ANY($1::uuid[])
	`, unique)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var email string
		var preferred *string
		var p UserProfile
		var privateName *string
        var claimed bool
		if err = rows.Scan(&id, &email, &p.UserHandle, &preferred, &p.Phone, &p.DeletedAt, &privateName, &claimed); err != nil {
			return nil, err
		}
		if preferred != nil {
			p.PreferredContact = *preferred
		}
		identity, err := administrativeIdentity(id, email, &p, privateName != nil && strings.TrimSpace(*privateName) != "" && ProfileFieldProblem("full_legal_name", privateName) == "", claimed)
		if err != nil {
			return nil, err
		}
		result[id] = identity
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) != len(unique) {
		return nil, errors.New("administrative identity reference unavailable")
	}
	return result, nil
}
