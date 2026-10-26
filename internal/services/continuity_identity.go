// Package services resolves continuity references independently of the bounded appointment directory.
// focodebase/fobackend/internal/services/continuity_identity.go

package services

import (
	"encoding/json"

	"github.com/PiccoloMondoC/focodebase/fobackend/internal/data"
	"github.com/google/uuid"
)

// Typed extraction excludes evidence and unrelated JSON strings. Audit IDs
// remain authoritative. Names are current projections, never fabricated
// event-time snapshots. Ceremony digests remain state-only.
func continuityIdentityIDs(raw json.RawMessage, history []data.ContinuityEvent) ([]uuid.UUID, error) {
	type reference struct {
		UserID       uuid.UUID  `json:"user_id"`
		AppointedBy  *uuid.UUID `json:"appointed_by"`
		DesignatedBy *uuid.UUID `json:"designated_by"`
	}
	var state struct {
		Incumbent  reference   `json:"incumbent"`
		CoRoot     *reference  `json:"co_root"`
		Successors []reference `json:"successors"`
		Honorary   []reference `json:"honorary"`
		Proposals  []struct {
			IncumbentID uuid.UUID `json:"incumbent_id"`
			SuccessorID uuid.UUID `json:"successor_id"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	ids := []uuid.UUID{state.Incumbent.UserID}
	add := func(r reference) {
		ids = append(ids, r.UserID)
		if r.AppointedBy != nil {
			ids = append(ids, *r.AppointedBy)
		}
		if r.DesignatedBy != nil {
			ids = append(ids, *r.DesignatedBy)
		}
	}
	if state.CoRoot != nil {
		add(*state.CoRoot)
	}
	for _, r := range state.Successors {
		add(r)
	}
	for _, r := range state.Honorary {
		add(r)
	}
	for _, p := range state.Proposals {
		ids = append(ids, p.IncumbentID, p.SuccessorID)
	}
	for _, e := range history {
		if e.ActorID != nil {
			ids = append(ids, *e.ActorID)
		}
		if e.TargetID != nil {
			ids = append(ids, *e.TargetID)
		}
	}
	return data.SortedGovernanceLockOrder(ids...), nil
}
