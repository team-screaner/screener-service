-- +goose Up
CREATE TABLE users (id uuid PRIMARY KEY,email text NOT NULL UNIQUE,name text NOT NULL,password_hash text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE api_tokens (id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES users(id),name text NOT NULL,token_hash text NOT NULL UNIQUE,scopes text[] NOT NULL,kind text NOT NULL CHECK(kind IN ('session','agent')),expires_at timestamptz NOT NULL,revoked_at timestamptz,created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX api_tokens_user ON api_tokens(user_id,id);
CREATE TABLE organizations (id uuid PRIMARY KEY,name text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE organization_members (organization_id uuid NOT NULL REFERENCES organizations(id),user_id uuid NOT NULL REFERENCES users(id),role text NOT NULL CHECK(role IN ('owner','admin','manager','member')),PRIMARY KEY(organization_id,user_id));
CREATE INDEX members_user ON organization_members(user_id,organization_id);
CREATE TABLE skill_categories (key text PRIMARY KEY,name text NOT NULL);
CREATE TABLE skills (id uuid PRIMARY KEY,key text NOT NULL,name text NOT NULL,description text NOT NULL DEFAULT '',category text NOT NULL REFERENCES skill_categories(key),scope text NOT NULL CHECK(scope IN ('system','organization','personal')),owner_id uuid,active boolean NOT NULL DEFAULT true,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),CHECK ((scope='system')=(owner_id IS NULL)),UNIQUE NULLS NOT DISTINCT(scope,owner_id,key));
CREATE TABLE evidence_signals(skill_id uuid NOT NULL REFERENCES skills(id),signal text NOT NULL,PRIMARY KEY(skill_id,signal));
CREATE TABLE fact_types (key text PRIMARY KEY,name text NOT NULL);
CREATE TABLE matrices(id uuid PRIMARY KEY,name text NOT NULL,description text NOT NULL DEFAULT '',scope text NOT NULL CHECK(scope IN ('system','organization','personal')),owner_id uuid,visibility text NOT NULL CHECK(visibility IN ('private','organization','public')),status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','archived')),parent_matrix_id uuid REFERENCES matrices(id),parent_version_id uuid,ignored_version_id uuid,created_at timestamptz NOT NULL DEFAULT now(),CHECK((scope='system')=(owner_id IS NULL)));
CREATE INDEX matrices_owner ON matrices(scope,owner_id,id);
CREATE TABLE matrix_versions(id uuid PRIMARY KEY,matrix_id uuid NOT NULL REFERENCES matrices(id),number integer NOT NULL CHECK(number>0),status text NOT NULL CHECK(status IN ('draft','published')),revision integer NOT NULL DEFAULT 1,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(matrix_id,number));
ALTER TABLE matrices ADD FOREIGN KEY(parent_version_id) REFERENCES matrix_versions(id);
ALTER TABLE matrices ADD FOREIGN KEY(ignored_version_id) REFERENCES matrix_versions(id);
CREATE TABLE levels(id uuid PRIMARY KEY,matrix_version_id uuid NOT NULL REFERENCES matrix_versions(id) ON DELETE CASCADE,key text NOT NULL,name text NOT NULL,description text NOT NULL DEFAULT '',position integer NOT NULL,UNIQUE(matrix_version_id,key),UNIQUE(matrix_version_id,id));
CREATE TABLE competency_groups(id uuid PRIMARY KEY,matrix_version_id uuid NOT NULL REFERENCES matrix_versions(id) ON DELETE CASCADE,key text NOT NULL,name text NOT NULL,weight double precision NOT NULL CHECK(weight>0 AND weight<'Infinity'::float8),UNIQUE(matrix_version_id,key),UNIQUE(matrix_version_id,id));
CREATE TABLE matrix_skills(id uuid PRIMARY KEY,matrix_version_id uuid NOT NULL REFERENCES matrix_versions(id) ON DELETE CASCADE,skill_id uuid NOT NULL REFERENCES skills(id),group_id uuid NOT NULL,weight double precision NOT NULL CHECK(weight>0 AND weight<'Infinity'::float8),required boolean NOT NULL,active boolean NOT NULL,position integer NOT NULL,FOREIGN KEY(matrix_version_id,group_id) REFERENCES competency_groups(matrix_version_id,id),UNIQUE(matrix_version_id,skill_id),UNIQUE(matrix_version_id,id));
CREATE TABLE requirements(id uuid PRIMARY KEY,matrix_version_id uuid NOT NULL REFERENCES matrix_versions(id) ON DELETE CASCADE,matrix_skill_id uuid NOT NULL,level_id uuid NOT NULL,description text NOT NULL CHECK(length(description)>0),required boolean NOT NULL,critical boolean NOT NULL,weight double precision NOT NULL CHECK(weight>0 AND weight<'Infinity'::float8),FOREIGN KEY(matrix_version_id,matrix_skill_id) REFERENCES matrix_skills(matrix_version_id,id),FOREIGN KEY(matrix_version_id,level_id) REFERENCES levels(matrix_version_id,id),UNIQUE(matrix_skill_id,level_id),UNIQUE(matrix_version_id,id));
CREATE TABLE user_matrices(id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES users(id),matrix_version_id uuid NOT NULL REFERENCES matrix_versions(id),current_level_id uuid NOT NULL,target_level_id uuid NOT NULL,status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','archived')),created_at timestamptz NOT NULL DEFAULT now(),FOREIGN KEY(matrix_version_id,current_level_id) REFERENCES levels(matrix_version_id,id),FOREIGN KEY(matrix_version_id,target_level_id) REFERENCES levels(matrix_version_id,id),UNIQUE(user_id,matrix_version_id));
CREATE INDEX user_matrices_user ON user_matrices(user_id,id);
CREATE TABLE personal_overrides(user_matrix_id uuid NOT NULL REFERENCES user_matrices(id),requirement_id uuid NOT NULL REFERENCES requirements(id),description text NOT NULL,reason text NOT NULL CHECK(length(reason)>0),updated_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_matrix_id,requirement_id));
CREATE TABLE evidence(id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES users(id),title text NOT NULL,description text NOT NULL,fact_type text NOT NULL REFERENCES fact_types(key),period_from date,period_to date,source text NOT NULL CHECK(source IN ('manual','github','gitlab','jira','confluence','linear','ai_import','api','other')),source_url text NOT NULL DEFAULT '',external_id text,status text NOT NULL CHECK(status IN ('suggested','accepted','rejected','manager_confirmed')),created_by uuid NOT NULL REFERENCES users(id),revision integer NOT NULL DEFAULT 1,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(user_id,source,external_id),CHECK(period_to>=period_from));
CREATE INDEX evidence_user ON evidence(user_id,id);
CREATE TABLE evidence_matches(id uuid PRIMARY KEY,evidence_id uuid NOT NULL REFERENCES evidence(id),skill_id uuid NOT NULL REFERENCES skills(id),requirement_id uuid REFERENCES requirements(id),confidence double precision NOT NULL CHECK(confidence>=0 AND confidence<=1),reason text NOT NULL,created_by uuid NOT NULL REFERENCES users(id),UNIQUE NULLS NOT DISTINCT(evidence_id,skill_id,requirement_id));
CREATE INDEX matches_evidence ON evidence_matches(evidence_id);
CREATE TABLE assessments(id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES users(id),user_matrix_id uuid NOT NULL REFERENCES user_matrices(id),matrix_version_id uuid NOT NULL REFERENCES matrix_versions(id),period text NOT NULL,type text NOT NULL CHECK(type IN ('self','manager','peer','confirmed')),created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX assessments_user ON assessments(user_id,user_matrix_id,id DESC);
CREATE TABLE assessment_items(assessment_id uuid NOT NULL REFERENCES assessments(id),requirement_id uuid NOT NULL REFERENCES requirements(id),score double precision NOT NULL CHECK(score>=0 AND score<=4),status text NOT NULL CHECK(status IN ('assessed','not_applicable')),comment text NOT NULL DEFAULT '',na_reason text NOT NULL DEFAULT '',requirement_snapshot text NOT NULL,PRIMARY KEY(assessment_id,requirement_id),CHECK(status<>'not_applicable' OR length(na_reason)>0));
CREATE TABLE assessment_evidence(assessment_id uuid NOT NULL,requirement_id uuid NOT NULL,evidence_id uuid NOT NULL REFERENCES evidence(id),PRIMARY KEY(assessment_id,requirement_id,evidence_id),FOREIGN KEY(assessment_id,requirement_id) REFERENCES assessment_items(assessment_id,requirement_id));
CREATE TABLE growth_plans(id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES users(id),user_matrix_id uuid NOT NULL REFERENCES user_matrices(id),target_level_id uuid NOT NULL REFERENCES levels(id),deadline date NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX growth_plans_user ON growth_plans(user_id,id);
CREATE TABLE growth_plan_items(id uuid PRIMARY KEY,plan_id uuid NOT NULL REFERENCES growth_plans(id),requirement_id uuid NOT NULL REFERENCES requirements(id),action text NOT NULL,status text NOT NULL CHECK(status IN ('planned','in_progress','done')));
CREATE TABLE growth_item_evidence(item_id uuid NOT NULL REFERENCES growth_plan_items(id) ON DELETE CASCADE,evidence_id uuid NOT NULL REFERENCES evidence(id),PRIMARY KEY(item_id,evidence_id));
CREATE TABLE idempotency(scope text NOT NULL,operation text NOT NULL,key text NOT NULL,request_hash text NOT NULL,response jsonb,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(scope,operation,key));
CREATE TABLE audit_log(id uuid NOT NULL,actor_id uuid REFERENCES users(id),operation text NOT NULL,resource_id text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(created_at,id)) PARTITION BY RANGE(created_at);
CREATE TABLE audit_log_default PARTITION OF audit_log DEFAULT;
CREATE INDEX audit_actor ON audit_log(actor_id,id);
-- +goose StatementBegin
CREATE FUNCTION protect_published_content() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v uuid;
BEGIN
 IF TG_OP='DELETE' THEN v=OLD.matrix_version_id; ELSE v=NEW.matrix_version_id; END IF;
 IF EXISTS(SELECT 1 FROM matrix_versions WHERE id=v AND status='published') THEN RAISE EXCEPTION 'published matrix is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
END $$;
-- +goose StatementEnd
CREATE TRIGGER levels_immutable BEFORE INSERT OR UPDATE OR DELETE ON levels FOR EACH ROW EXECUTE FUNCTION protect_published_content();
CREATE TRIGGER groups_immutable BEFORE INSERT OR UPDATE OR DELETE ON competency_groups FOR EACH ROW EXECUTE FUNCTION protect_published_content();
CREATE TRIGGER skills_immutable BEFORE INSERT OR UPDATE OR DELETE ON matrix_skills FOR EACH ROW EXECUTE FUNCTION protect_published_content();
CREATE TRIGGER requirements_immutable BEFORE INSERT OR UPDATE OR DELETE ON requirements FOR EACH ROW EXECUTE FUNCTION protect_published_content();
-- +goose StatementBegin
CREATE FUNCTION protect_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'snapshot is immutable' USING ERRCODE='23514'; END $$;
-- +goose StatementEnd
CREATE TRIGGER assessments_immutable BEFORE UPDATE OR DELETE ON assessments FOR EACH ROW EXECUTE FUNCTION protect_snapshot();
CREATE TRIGGER assessment_items_immutable BEFORE UPDATE OR DELETE ON assessment_items FOR EACH ROW EXECUTE FUNCTION protect_snapshot();
CREATE TRIGGER assessment_evidence_immutable BEFORE UPDATE OR DELETE ON assessment_evidence FOR EACH ROW EXECUTE FUNCTION protect_snapshot();
-- +goose Down
DROP TABLE audit_log,idempotency,growth_item_evidence,growth_plan_items,growth_plans,assessment_evidence,assessment_items,assessments,evidence_matches,evidence,personal_overrides,user_matrices;
ALTER TABLE matrices DROP CONSTRAINT matrices_parent_version_id_fkey;
ALTER TABLE matrices DROP CONSTRAINT matrices_ignored_version_id_fkey;
DROP TABLE requirements,matrix_skills,competency_groups,levels,matrix_versions,matrices,evidence_signals,skills,skill_categories,fact_types,organization_members,organizations,api_tokens,users;
DROP FUNCTION protect_snapshot();
DROP FUNCTION protect_published_content();
