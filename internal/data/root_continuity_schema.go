// focodebase/fobackend/internal/data/root_continuity_schema.go
// Additive continuity schema. Existing Root ledger and installation receipt
// retain their exact shapes. Every state change commits with evidence and audit.
package data

const rootContinuitySchema = `
CREATE TABLE IF NOT EXISTS root_co_holder (
 singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton),
 user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE RESTRICT,
 appointed_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 appointed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK(user_id <> appointed_by)
);
CREATE TABLE IF NOT EXISTS root_successor_designations (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
 rank SMALLINT NOT NULL CHECK(rank BETWEEN 1 AND 3),
 designated_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 designated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(rank) DEFERRABLE INITIALLY DEFERRED,
 CHECK(user_id <> designated_by)
);
CREATE TABLE IF NOT EXISTS root_succession_proposals (
 id UUID PRIMARY KEY,
 incumbent_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 successor_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 generation INTEGER NOT NULL CHECK(generation >= 1),
 status TEXT NOT NULL CHECK(status IN ('pending','accepted','cancelled','expired','superseded')),
 proposed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 expires_at TIMESTAMPTZ NOT NULL,
 resolved_at TIMESTAMPTZ,
 CHECK(incumbent_id <> successor_id),
 CHECK(expires_at > proposed_at AND expires_at <= proposed_at + INTERVAL '72 hours'),
 CHECK((status = 'pending') = (resolved_at IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS ux_root_pending_proposal ON root_succession_proposals((TRUE)) WHERE status='pending';
CREATE TABLE IF NOT EXISTS root_honorary_standing (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE RESTRICT,
 succession_generation INTEGER NOT NULL UNIQUE CHECK(succession_generation>1) REFERENCES root_succession_events(succession_generation) ON DELETE RESTRICT,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS root_continuity_events (
 id UUID PRIMARY KEY,
 operation TEXT NOT NULL CHECK(operation IN ('designate','reorder','undesignate','appoint_co_root','remove_co_root','replace_co_root','propose','cancel','accept','recovery','consultant_grant','consultant_revoke')),
 actor_id UUID REFERENCES users(id) ON DELETE RESTRICT,
 target_id UUID REFERENCES users(id) ON DELETE RESTRICT,
 transaction_id BIGINT NOT NULL DEFAULT txid_current() UNIQUE,
 before_state JSONB NOT NULL,
 after_state JSONB NOT NULL,
 evidence JSONB NOT NULL,
 occurred_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK((operation = 'recovery' OR operation IN ('consultant_grant','consultant_revoke')) = (actor_id IS NULL))
);
CREATE TABLE IF NOT EXISTS root_transfer_evidence (
 ledger_id UUID PRIMARY KEY REFERENCES root_succession_events(id) ON DELETE RESTRICT,
 ceremony_id UUID NOT NULL UNIQUE REFERENCES root_continuity_events(id) DEFERRABLE INITIALLY DEFERRED,
 proposal_id UUID UNIQUE REFERENCES root_succession_proposals(id) ON DELETE RESTRICT,
 board_reference TEXT CHECK(length(board_reference) BETWEEN 1 AND 200),
 cio_reference TEXT CHECK(length(cio_reference) BETWEEN 1 AND 200),
 executor TEXT CHECK(length(executor) BETWEEN 1 AND 200),
 reason TEXT CHECK(reason IN ('incumbent_incapacity','incumbent_unreachable','board_ordered_removal')),
 predecessor_disposition TEXT NOT NULL CHECK(predecessor_disposition IN ('consumer','admin','super_admin')),
 departure_without_conflict BOOLEAN NOT NULL,
 CHECK((proposal_id IS NOT NULL AND board_reference IS NULL AND cio_reference IS NULL AND executor IS NULL AND reason IS NULL AND departure_without_conflict AND predecessor_disposition='consumer')
 OR (proposal_id IS NULL AND board_reference IS NOT NULL AND cio_reference IS NOT NULL AND executor IS NOT NULL AND reason IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS root_consultant_grants (
 id UUID PRIMARY KEY,
 user_id UUID NOT NULL REFERENCES root_honorary_standing(user_id) ON DELETE RESTRICT,
 issue_reference TEXT NOT NULL CHECK(length(issue_reference) BETWEEN 1 AND 200),
 cio_reference TEXT NOT NULL CHECK(length(cio_reference) BETWEEN 1 AND 200),
 executor TEXT NOT NULL CHECK(length(executor) BETWEEN 1 AND 200),
 capabilities TEXT[] NOT NULL CHECK(cardinality(capabilities)>0 AND capabilities <@ ARRAY['read_continuity','read_administrators','read_governance_history']::TEXT[]),
 granted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 expires_at TIMESTAMPTZ NOT NULL CHECK(expires_at>granted_at AND expires_at<=granted_at+INTERVAL '7 days'),
 revoked_at TIMESTAMPTZ
);

CREATE OR REPLACE FUNCTION public.root_continuity_state() RETURNS JSONB AS $$
 SELECT jsonb_build_object(
 'incumbent',(SELECT jsonb_build_object('user_id',user_id,'generation',succession_generation) FROM public.root_super_admin WHERE singleton),
 'co_root',(SELECT jsonb_build_object('user_id',user_id,'appointed_by',appointed_by,'appointed_at',appointed_at) FROM public.root_co_holder WHERE singleton),
 'successors',COALESCE((SELECT jsonb_agg(to_jsonb(d) ORDER BY rank) FROM public.root_successor_designations d),'[]'::jsonb),
 'proposals',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY proposed_at,id) FROM public.root_succession_proposals p),'[]'::jsonb),
 'honorary',COALESCE((SELECT jsonb_agg(to_jsonb(h) ORDER BY user_id) FROM public.root_honorary_standing h),'[]'::jsonb),
 'consultants',COALESCE((SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM public.root_consultant_grants g),'[]'::jsonb))
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION public.protect_continuity_state() RETURNS TRIGGER AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(7301195001);
 IF TG_OP='TRUNCATE' OR COALESCE(current_setting('sagrenti.continuity_operation',true),'') NOT IN
 ('designate','reorder','undesignate','appoint_co_root','remove_co_root','replace_co_root','propose','cancel','accept','recovery','consultant_grant','consultant_revoke') THEN
 RAISE EXCEPTION 'continuity ceremony required' USING ERRCODE='SG007'; END IF;
 IF TG_TABLE_NAME='root_honorary_standing' THEN
 IF TG_OP<>'INSERT' OR current_setting('sagrenti.continuity_operation',true) NOT IN ('accept','recovery') THEN
 RAISE EXCEPTION 'honorary standing follows an authorized departure' USING ERRCODE='SG007'; END IF;
 END IF;
 IF TG_TABLE_NAME='root_co_holder' THEN
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION 'co-Root replacement is a distinct ceremony' USING ERRCODE='SG007'; END IF;
 IF TG_OP='INSERT' AND (current_setting('sagrenti.continuity_operation',true) NOT IN ('appoint_co_root','replace_co_root') OR NEW.appointed_by IS DISTINCT FROM (SELECT user_id FROM public.root_super_admin WHERE singleton)) THEN
 RAISE EXCEPTION 'only incumbent appoints the co-Root' USING ERRCODE='SG007'; END IF;
 END IF;
 IF TG_TABLE_NAME='root_successor_designations' THEN
 IF TG_OP='INSERT' AND (current_setting('sagrenti.continuity_operation',true)<>'designate' OR NEW.designated_by IS DISTINCT FROM (SELECT user_id FROM public.root_super_admin WHERE singleton)) THEN
 RAISE EXCEPTION 'only incumbent designates successors' USING ERRCODE='SG007'; END IF;
 IF TG_OP='UPDATE' AND (NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.designated_by IS DISTINCT FROM OLD.designated_by OR NEW.designated_at IS DISTINCT FROM OLD.designated_at) THEN
 RAISE EXCEPTION 'designation identity is immutable' USING ERRCODE='SG007'; END IF;
 END IF;
 IF TG_TABLE_NAME='root_succession_proposals' THEN
 IF TG_OP='INSERT' AND current_setting('sagrenti.continuity_operation',true)<>'propose' THEN RAISE EXCEPTION 'incumbent proposal ceremony required' USING ERRCODE='SG007'; END IF;
 IF TG_OP='UPDATE' AND NOT (
 (NEW.status='accepted' AND current_setting('sagrenti.continuity_operation',true)='accept') OR
 (NEW.status='cancelled' AND current_setting('sagrenti.continuity_operation',true)='cancel') OR
 (NEW.status='expired' AND current_setting('sagrenti.continuity_operation',true)='propose' AND OLD.expires_at<=clock_timestamp()) OR
 (NEW.status='superseded' AND current_setting('sagrenti.continuity_operation',true) IN ('remove_co_root','replace_co_root','recovery'))) THEN RAISE EXCEPTION 'proposal resolution ceremony mismatch' USING ERRCODE='SG007'; END IF;
 IF TG_OP='DELETE' OR (TG_OP='INSERT' AND NEW.status<>'pending') THEN RAISE EXCEPTION 'invalid proposal lifecycle' USING ERRCODE='SG007'; END IF;
 IF TG_OP='UPDATE' AND (OLD.status<>'pending' OR NEW.status='pending' OR NEW.id<>OLD.id OR NEW.incumbent_id<>OLD.incumbent_id OR NEW.successor_id<>OLD.successor_id OR NEW.generation<>OLD.generation OR NEW.proposed_at<>OLD.proposed_at OR NEW.expires_at<>OLD.expires_at) THEN
 RAISE EXCEPTION 'proposal immutable or already resolved' USING ERRCODE='SG007'; END IF;
 END IF;
 IF TG_TABLE_NAME='root_consultant_grants' AND TG_OP<>'INSERT' THEN
 IF TG_OP<>'UPDATE' THEN RAISE EXCEPTION 'consultant grant retained' USING ERRCODE='SG007'; END IF;
 IF OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR (to_jsonb(OLD)-'revoked_at')<>(to_jsonb(NEW)-'revoked_at') THEN RAISE EXCEPTION 'only revocation permitted' USING ERRCODE='SG007'; END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$ LANGUAGE plpgsql;
DO $$ DECLARE t TEXT; BEGIN
 FOREACH t IN ARRAY ARRAY['root_co_holder','root_successor_designations','root_succession_proposals','root_honorary_standing','root_consultant_grants'] LOOP
 EXECUTE format('DROP TRIGGER IF EXISTS protect_continuity_state ON %I',t);
 EXECUTE format('CREATE TRIGGER protect_continuity_state BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION public.protect_continuity_state()',t);
 EXECUTE format('DROP TRIGGER IF EXISTS protect_continuity_truncate ON %I',t);
 EXECUTE format('CREATE TRIGGER protect_continuity_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION public.protect_continuity_state()',t);
 END LOOP;
END $$;
CREATE OR REPLACE FUNCTION public.protect_continuity_evidence() RETURNS TRIGGER AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'continuity evidence is append-only' USING ERRCODE='SG007'; END IF;
 IF TG_TABLE_NAME='root_continuity_events' AND (NEW.transaction_id<>txid_current() OR NEW.operation IS DISTINCT FROM current_setting('sagrenti.continuity_operation',true)) THEN
 RAISE EXCEPTION 'continuity event marker mismatch' USING ERRCODE='SG007'; END IF;
 RETURN NEW;
END; $$ LANGUAGE plpgsql;
DO $$ DECLARE t TEXT; BEGIN
 FOREACH t IN ARRAY ARRAY['root_continuity_events','root_transfer_evidence'] LOOP
 EXECUTE format('DROP TRIGGER IF EXISTS protect_continuity_evidence ON %I',t);
 EXECUTE format('CREATE TRIGGER protect_continuity_evidence BEFORE INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION public.protect_continuity_evidence()',t);
 EXECUTE format('DROP TRIGGER IF EXISTS protect_continuity_evidence_truncate ON %I',t);
 EXECUTE format('CREATE TRIGGER protect_continuity_evidence_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION public.protect_continuity_evidence()',t);
 END LOOP;
END $$;

-- Keep the original generation-1 entitlement check intact; only non-bootstrap
-- ledger inserts take the continuity branch. This replaces the Drop 2 function.
CREATE OR REPLACE FUNCTION public.require_installation_bootstrap_ceremony() RETURNS TRIGGER AS $$
DECLARE target UUID; r public.root_super_admin%ROWTYPE;
BEGIN
 PERFORM pg_advisory_xact_lock(7301195001);
 IF TG_TABLE_NAME='root_succession_events' AND NEW.mechanism<>'bootstrap' THEN
 SELECT * INTO r FROM public.root_super_admin WHERE singleton;
 IF r.user_id IS NULL OR NEW.succession_generation<>r.succession_generation+1 OR NEW.predecessor_user_id<>r.user_id OR NEW.successor_user_id=r.user_id
 OR NOT EXISTS(SELECT 1 FROM public.platform_installation)
 OR NOT ((NEW.mechanism='voluntary_transfer' AND current_setting('sagrenti.continuity_operation',true)='accept') OR (NEW.mechanism='emergency_succession' AND current_setting('sagrenti.continuity_operation',true)='recovery')) THEN
 RAISE EXCEPTION 'invalid continuity ledger generation' USING ERRCODE='SG007'; END IF;
 RETURN NEW;
 END IF;
 IF current_setting('sagrenti.installation_ceremony',true) IS DISTINCT FROM 'complete' OR EXISTS(SELECT 1 FROM public.platform_installation) THEN
 RAISE EXCEPTION 'Root bootstrap requires email completion' USING ERRCODE='SG006'; END IF;
 IF TG_TABLE_NAME='root_super_admin' THEN target:=NEW.user_id;
 ELSE IF NEW.mechanism<>'bootstrap' OR NEW.succession_generation<>1 THEN RAISE EXCEPTION 'invalid bootstrap generation' USING ERRCODE='SG006'; END IF; target:=NEW.successor_user_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM public.platform_bootstrap_claims c JOIN public.users u ON u.email=c.claimant_email WHERE u.id=target AND c.status='pending' AND c.expires_at>clock_timestamp()) THEN
 RAISE EXCEPTION 'Root bootstrap requires its pending entitlement' USING ERRCODE='SG006'; END IF;
 RETURN NEW;
END; $$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION public.protect_root_super_admin_row() RETURNS TRIGGER AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(7301195001);
 IF TG_OP<>'UPDATE' THEN RAISE EXCEPTION 'Root cannot be removed' USING ERRCODE='SG001'; END IF;
 IF NEW.singleton IS DISTINCT FROM OLD.singleton OR NEW.established_at IS DISTINCT FROM OLD.established_at OR NEW.succession_generation<>OLD.succession_generation+1 OR NEW.user_id=OLD.user_id OR NOT EXISTS(
 SELECT 1 FROM public.root_succession_events l WHERE l.succession_generation=NEW.succession_generation AND l.successor_user_id=NEW.user_id AND l.predecessor_user_id=OLD.user_id
 AND ((l.mechanism='voluntary_transfer' AND current_setting('sagrenti.continuity_operation',true)='accept') OR (l.mechanism='emergency_succession' AND current_setting('sagrenti.continuity_operation',true)='recovery'))) THEN
 RAISE EXCEPTION 'Root transfer requires its ceremony ledger' USING ERRCODE='SG007'; END IF;
 PERFORM public.validate_root_account(NEW.user_id);
 RETURN NEW;
END; $$ LANGUAGE plpgsql;
CREATE OR REPLACE FUNCTION public.validate_root_account(candidate UUID) RETURNS VOID AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM public.users u JOIN public.administrative_appointments a ON a.user_id=u.id
 JOIN public.user_role_assignments ura ON ura.user_id=u.id AND ura.is_primary AND ura.deleted_at IS NULL
 JOIN public.roles ro ON ro.id=ura.role_id AND ro.name='super_admin' AND ro.is_active AND ro.deleted_at IS NULL
 WHERE u.id=candidate AND u.is_active AND u.deleted_at IS NULL AND a.level='super_admin' AND a.status='active') THEN
 RAISE EXCEPTION 'Root standing requires live account appointment and role' USING ERRCODE='SG007'; END IF;
END; $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION public.assert_root_continuity() RETURNS TRIGGER AS $$
DECLARE e public.root_continuity_events%ROWTYPE; n INTEGER; hi INTEGER; lo INTEGER; candidate UUID;
BEGIN
 SELECT * INTO e FROM public.root_continuity_events WHERE transaction_id=txid_current();
 IF e.id IS NULL OR e.after_state IS DISTINCT FROM public.root_continuity_state() OR NOT EXISTS(
 SELECT 1 FROM public.audit_logs a JOIN public.actions ac ON ac.id=a.action_id JOIN public.entity_types et ON et.id=a.entity_type_id
 WHERE a.entity_id=e.id::text AND a.user_id IS NOT DISTINCT FROM e.actor_id AND ac.name='root_continuity' AND et.name='root_super_admin' AND a.outcome='succeeded') THEN
 RAISE EXCEPTION 'continuity change must commit with matching evidence and audit' USING ERRCODE='SG007'; END IF;
 IF e.operation IN ('recovery','consultant_grant','consultant_revoke') AND (
 COALESCE(e.evidence->'approval'->>'cio_signature','') !~ '^[0-9a-f]{128}$' OR
 COALESCE(e.evidence->'approval'->>'cio_key_fingerprint','') !~ '^[0-9a-f]{64}$' OR
 (e.operation='recovery' AND (COALESCE(e.evidence->'approval'->>'board_signature','') !~ '^[0-9a-f]{128}$' OR COALESCE(e.evidence->'approval'->>'board_key_fingerprint','') !~ '^[0-9a-f]{64}$'))) THEN
 RAISE EXCEPTION 'operator ceremony requires recorded verified approvals' USING ERRCODE='SG007'; END IF;
 IF TG_TABLE_NAME='root_honorary_standing' THEN
 IF NOT EXISTS(SELECT 1 FROM public.root_succession_events l JOIN public.root_transfer_evidence te ON te.ledger_id=l.id
 WHERE l.succession_generation=NEW.succession_generation AND l.predecessor_user_id=NEW.user_id AND te.ceremony_id=e.id AND te.departure_without_conflict) THEN
 RAISE EXCEPTION 'honorary standing requires non-conflict departure evidence' USING ERRCODE='SG007'; END IF;
 END IF;
 IF e.operation NOT IN ('accept','recovery','consultant_grant','consultant_revoke') AND e.actor_id IS DISTINCT FROM (SELECT user_id FROM public.root_super_admin WHERE singleton) THEN
 RAISE EXCEPTION 'incumbent authority required' USING ERRCODE='SG007'; END IF;
 IF e.operation='accept' AND e.actor_id IS DISTINCT FROM (SELECT user_id FROM public.root_super_admin WHERE singleton) THEN
 RAISE EXCEPTION 'successor must accept personally' USING ERRCODE='SG007'; END IF;
 SELECT count(*),min(rank),max(rank) INTO n,lo,hi FROM public.root_successor_designations;
 IF n>3 OR (n>0 AND (lo<>1 OR hi<>n)) THEN RAISE EXCEPTION 'successor ranks must be contiguous' USING ERRCODE='SG007'; END IF;
 IF EXISTS(SELECT 1 FROM public.root_successor_designations d JOIN public.root_super_admin r ON d.user_id=r.user_id)
 OR EXISTS(SELECT 1 FROM public.root_co_holder c JOIN public.root_super_admin r ON c.user_id=r.user_id)
 OR EXISTS(SELECT 1 FROM public.root_co_holder c JOIN public.root_successor_designations d ON c.user_id=d.user_id) THEN
 RAISE EXCEPTION 'serving Root standings must be distinct' USING ERRCODE='SG007'; END IF;
 FOR candidate IN SELECT user_id FROM public.root_co_holder UNION SELECT user_id FROM public.root_successor_designations LOOP PERFORM public.validate_root_account(candidate); END LOOP;
 IF EXISTS(SELECT 1 FROM public.root_succession_proposals p WHERE p.status='pending' AND NOT EXISTS(
 SELECT 1 FROM public.root_super_admin r JOIN public.root_co_holder c ON c.user_id=p.successor_id WHERE r.user_id=p.incumbent_id AND r.succession_generation=p.generation)) THEN
 RAISE EXCEPTION 'pending proposal requires current incumbent and appointed co-Root' USING ERRCODE='SG007'; END IF;
 IF e.operation IN ('accept','recovery') AND NOT EXISTS(
 SELECT 1 FROM public.root_super_admin r JOIN public.root_succession_events l ON l.succession_generation=r.succession_generation
 JOIN public.root_transfer_evidence te ON te.ledger_id=l.id WHERE te.ceremony_id=e.id AND
 ((e.operation='accept' AND l.mechanism='voluntary_transfer' AND EXISTS(SELECT 1 FROM public.root_succession_proposals p WHERE p.id=te.proposal_id AND p.status='accepted' AND p.incumbent_id=l.predecessor_user_id AND p.successor_id=l.successor_user_id AND p.generation+1=l.succession_generation AND p.resolved_at<p.expires_at))
 OR (e.operation='recovery' AND l.mechanism='emergency_succession' AND te.proposal_id IS NULL))) THEN
 RAISE EXCEPTION 'transfer evidence does not establish current Root' USING ERRCODE='SG007'; END IF;
 RETURN NULL;
END; $$ LANGUAGE plpgsql;
DO $$ DECLARE t TEXT; BEGIN
 FOREACH t IN ARRAY ARRAY['root_co_holder','root_successor_designations','root_succession_proposals','root_honorary_standing','root_consultant_grants','root_continuity_events','root_transfer_evidence'] LOOP
 EXECUTE format('DROP TRIGGER IF EXISTS assert_root_continuity ON %I',t);
 EXECUTE format('CREATE CONSTRAINT TRIGGER assert_root_continuity AFTER INSERT OR UPDATE OR DELETE ON %I DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.assert_root_continuity()',t);
 END LOOP;
END $$;
CREATE OR REPLACE FUNCTION public.assert_root_ledger_continuity() RETURNS TRIGGER AS $$
BEGIN
 IF NEW.mechanism<>'bootstrap' AND NOT EXISTS(
 SELECT 1 FROM public.root_transfer_evidence te JOIN public.root_continuity_events e ON e.id=te.ceremony_id
 JOIN public.root_super_admin r ON r.succession_generation=NEW.succession_generation AND r.user_id=NEW.successor_user_id
 WHERE te.ledger_id=NEW.id AND e.transaction_id=txid_current()) THEN
 RAISE EXCEPTION 'new ledger generation requires completed continuity evidence' USING ERRCODE='SG007'; END IF;
 RETURN NULL;
END; $$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS assert_root_ledger_continuity ON root_succession_events;
CREATE CONSTRAINT TRIGGER assert_root_ledger_continuity AFTER INSERT ON root_succession_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.assert_root_ledger_continuity();
DROP TRIGGER IF EXISTS assert_root_transfer ON root_super_admin;
CREATE CONSTRAINT TRIGGER assert_root_transfer AFTER UPDATE ON root_super_admin DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.assert_root_continuity();

-- Co-Root and designated successors cannot silently lose their required
-- account, appointment or role. The dedicated ceremony removes standing first.
CREATE OR REPLACE FUNCTION public.protect_root_continuity_subject() RETURNS TRIGGER AS $$
DECLARE candidate UUID;
BEGIN
 IF TG_TABLE_NAME='users' THEN candidate:=OLD.id; ELSE candidate:=OLD.user_id; END IF;
 IF EXISTS(SELECT 1 FROM public.root_co_holder WHERE user_id=candidate) OR EXISTS(SELECT 1 FROM public.root_successor_designations WHERE user_id=candidate) THEN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'remove continuity standing first' USING ERRCODE='SG007'; END IF;
 IF TG_TABLE_NAME='users' THEN
 IF NEW.id IS DISTINCT FROM OLD.id OR NOT NEW.is_active OR NEW.deleted_at IS NOT NULL THEN RAISE EXCEPTION 'continuity account protected' USING ERRCODE='SG007'; END IF;
 ELSIF TG_TABLE_NAME='administrative_appointments' THEN
 IF NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.level<>'super_admin' OR NEW.status<>'active' THEN RAISE EXCEPTION 'continuity appointment protected' USING ERRCODE='SG007'; END IF;
 ELSE
 IF NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.role_id IS DISTINCT FROM OLD.role_id OR NEW.deleted_at IS DISTINCT FROM OLD.deleted_at OR NEW.is_primary IS DISTINCT FROM OLD.is_primary THEN RAISE EXCEPTION 'continuity role protected' USING ERRCODE='SG007'; END IF;
 END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END; $$ LANGUAGE plpgsql;
DO $$ DECLARE t TEXT; BEGIN
 FOREACH t IN ARRAY ARRAY['users','administrative_appointments','user_role_assignments'] LOOP
 EXECUTE format('DROP TRIGGER IF EXISTS protect_root_continuity_subject ON %I',t);
 EXECUTE format('CREATE TRIGGER protect_root_continuity_subject BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION public.protect_root_continuity_subject()',t);
 END LOOP;
END $$;
`
