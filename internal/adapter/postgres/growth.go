package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"github.com/team-screaner/screener-service/internal/domain"
)

type assessmentItem struct {
	RequirementID string   `json:"requirement_id"`
	Score         float64  `json:"score"`
	Status        string   `json:"status"`
	Comment       string   `json:"comment"`
	NAReason      string   `json:"na_reason"`
	EvidenceIDs   []string `json:"evidence_ids"`
}
type assessmentInput struct {
	ReviewedAssessmentID string           `json:"reviewed_assessment_id"`
	UserMatrixID         string           `json:"user_matrix_id"`
	Period               string           `json:"period"`
	Type                 string           `json:"type"`
	Items                []assessmentItem `json:"items"`
}

func userMatrix(ctx context.Context, tx *sql.Tx, user, id string) (domain.Result, error) {
	if id == "" {
		rows, err := jsonRows(ctx, tx, `SELECT to_jsonb(u) FROM user_matrices u WHERE user_id=$1 AND status='active' ORDER BY id LIMIT 2`, user)
		if err != nil {
			return nil, err
		}
		if len(rows) != 1 {
			return nil, domain.Invalid("Specify user_matrix_id; expected exactly one active matrix")
		}
		return rows[0], nil
	}
	return jsonRow(ctx, tx, `SELECT to_jsonb(u) FROM user_matrices u WHERE id=$1 AND user_id=$2`, id, user)
}
func validateUserRequirement(ctx context.Context, tx *sql.Tx, um, req string) (string, error) {
	var description string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(o.description,r.description) FROM requirements r JOIN user_matrices u ON u.matrix_version_id=r.matrix_version_id LEFT JOIN personal_overrides o ON o.user_matrix_id=u.id AND o.requirement_id=r.id WHERE u.id=$1 AND r.id=$2`, um, req).Scan(&description)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.Invalid("Requirement does not belong to user matrix")
	}
	return description, err
}
func validateEvidenceLink(ctx context.Context, tx *sql.Tx, user, evidence, req string) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM evidence e JOIN evidence_matches m ON m.evidence_id=e.id WHERE e.id=$1 AND e.user_id=$2 AND e.status IN ('accepted','manager_confirmed') AND m.requirement_id=$3)`, evidence, user, req).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return domain.Invalid("Evidence must be accepted, owned and matched to the requirement")
	}
	return nil
}
func handleGrowth(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	switch c.Operation {
	case "createUserMatrix":
		in, err := decode[struct {
			Version string `json:"matrix_version_id"`
			Current string `json:"current_level_id"`
			Target  string `json:"target_level_id"`
		}](c.Body)
		if err != nil {
			return nil, err
		}
		v, err := accessibleVersion(ctx, tx, in.Version, c.Actor.UserID)
		if err != nil {
			return nil, err
		}
		if v["status"] != "published" {
			return nil, domain.Invalid("Only published matrix versions can be assigned")
		}
		var current, target int
		err = tx.QueryRowContext(ctx, `SELECT a.position,b.position FROM levels a,levels b WHERE a.id=$1 AND b.id=$2 AND a.matrix_version_id=$3 AND b.matrix_version_id=$3`, in.Current, in.Target, in.Version).Scan(&current, &target)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.Invalid("Levels must belong to matrix version")
		}
		if err != nil {
			return nil, err
		}
		if target < current {
			return nil, domain.Invalid("Target level must not precede current level")
		}
		id := ID()
		_, err = tx.ExecContext(ctx, `INSERT INTO user_matrices(id,user_id,matrix_version_id,current_level_id,target_level_id) VALUES($1,$2,$3,$4,$5)`, id, c.Actor.UserID, in.Version, in.Current, in.Target)
		if err != nil {
			return nil, err
		}
		return userMatrix(ctx, tx, c.Actor.UserID, id)
	case "listUserMatrices":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		items, err := jsonRows(ctx, tx, `SELECT to_jsonb(u) FROM user_matrices u WHERE user_id=$1 AND id>$2 ORDER BY id LIMIT $3`, c.Actor.UserID, after, limit+1)
		return paged(c, items, limit), err
	case "setOverride":
		in, err := decode[struct {
			UserMatrixID        string `json:"user_matrix_id"`
			RequirementID       string `json:"requirement_id"`
			Description, Reason string
		}](c.Body)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Description) == "" || strings.TrimSpace(in.Reason) == "" || len(in.Description) > 10000 {
			return nil, domain.Invalid("Override description and reason required")
		}
		if _, err = userMatrix(ctx, tx, c.Actor.UserID, in.UserMatrixID); err != nil {
			return nil, err
		}
		if _, err = validateUserRequirement(ctx, tx, in.UserMatrixID, in.RequirementID); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO personal_overrides(user_matrix_id,requirement_id,description,reason) VALUES($1,$2,$3,$4) ON CONFLICT(user_matrix_id,requirement_id) DO UPDATE SET description=excluded.description,reason=excluded.reason,updated_at=now()`, in.UserMatrixID, in.RequirementID, in.Description, in.Reason)
		return domain.Result{"user_matrix_id": in.UserMatrixID, "requirement_id": in.RequirementID, "description": in.Description, "reason": in.Reason}, err
	case "createAssessment":
		return createAssessment(ctx, tx, c)
	case "getAssessment":
		return assessmentData(ctx, tx, c.ID, c.Actor.UserID)
	case "listAssessments":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		um := c.Query.Get("user_matrix_id")
		if um != "" {
			if _, err = userMatrix(ctx, tx, c.Actor.UserID, um); err != nil {
				return nil, err
			}
		}
		items, err := jsonRows(ctx, tx, `SELECT to_jsonb(a) FROM assessments a WHERE user_id=$1 AND id>$2 AND ($3::uuid IS NULL OR user_matrix_id=$3) ORDER BY id LIMIT $4`, c.Actor.UserID, after, nullable(um), limit+1)
		return paged(c, items, limit), err
	case "getGrowthContext":
		return growthContext(ctx, tx, c)
	case "getGaps", "getRadar":
		return readiness(ctx, tx, c)
	case "createGrowthPlan", "updateGrowthPlan":
		return saveGrowthPlan(ctx, tx, c)
	case "listGrowthPlans":
		limit, after, err := page(c)
		if err != nil {
			return nil, err
		}
		um := c.Query.Get("user_matrix_id")
		if um != "" {
			if _, err = userMatrix(ctx, tx, c.Actor.UserID, um); err != nil {
				return nil, err
			}
		}
		items, err := jsonRows(ctx, tx, `SELECT to_jsonb(p)||jsonb_build_object('items',COALESCE((SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('evidence_ids',COALESCE((SELECT jsonb_agg(evidence_id) FROM growth_item_evidence WHERE item_id=i.id),'[]'))) FROM growth_plan_items i WHERE i.plan_id=p.id),'[]')) FROM growth_plans p WHERE user_id=$1 AND id>$2 AND ($3::uuid IS NULL OR user_matrix_id=$3) ORDER BY id LIMIT $4`, c.Actor.UserID, after, nullable(um), limit+1)
		return paged(c, items, limit), err
	default:
		return nil, domain.NotFound()
	}
}
func createAssessment(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[assessmentInput](c.Body)
	if err != nil {
		return nil, err
	}
	if (in.Type != "self" && in.Type != "manager") || strings.TrimSpace(in.Period) == "" || len(in.Period) > 100 || len(in.Items) < 1 || len(in.Items) > 10000 {
		return nil, domain.Invalid("Assessment requires period and 1..10000 items")
	}
	var u domain.Result
	if in.Type == "manager" {
		u, err = managerAssessmentSubject(ctx, tx, c, in)
	} else {
		if in.ReviewedAssessmentID != "" {
			return nil, domain.Invalid("Self assessment cannot review another assessment")
		}
		u, err = userMatrix(ctx, tx, c.Actor.UserID, in.UserMatrixID)
	}
	if err != nil {
		return nil, err
	}
	owner, ok := u["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid assignment owner")
	}
	id := ID()
	_, err = tx.ExecContext(ctx, `INSERT INTO assessments(id,user_id,user_matrix_id,matrix_version_id,period,type,assessor_id,reviewed_assessment_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, owner, in.UserMatrixID, u["matrix_version_id"], in.Period, in.Type, c.Actor.UserID, nullable(in.ReviewedAssessmentID))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range in.Items {
		if seen[item.RequirementID] || math.IsNaN(item.Score) || math.IsInf(item.Score, 0) || item.Score < 0 || item.Score > 4 || (item.Status != "assessed" && item.Status != "not_applicable") || item.Status == "not_applicable" && strings.TrimSpace(item.NAReason) == "" {
			return nil, domain.Invalid("Invalid assessment item or duplicate requirement")
		}
		if item.Status == "not_applicable" && item.Score != 0 {
			return nil, domain.Invalid("Not applicable requirements must have score zero")
		}
		if item.Status == "assessed" && item.Score > 0 && len(item.EvidenceIDs) == 0 {
			return nil, domain.Invalid("Positive assessment scores require accepted evidence matched to the requirement")
		}
		seen[item.RequirementID] = true
		description, err := validateUserRequirement(ctx, tx, in.UserMatrixID, item.RequirementID)
		if err != nil {
			return nil, err
		}
		if in.Type == "manager" {
			var snapshot string
			snapshotErr := tx.QueryRowContext(ctx, `SELECT requirement_snapshot FROM assessment_items WHERE assessment_id=$1 AND requirement_id=$2`, in.ReviewedAssessmentID, item.RequirementID).Scan(&snapshot)
			if snapshotErr != nil && !errors.Is(snapshotErr, sql.ErrNoRows) {
				return nil, snapshotErr
			}
			if snapshotErr == nil {
				description = snapshot
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assessment_items(assessment_id,requirement_id,score,status,comment,na_reason,requirement_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, item.RequirementID, item.Score, item.Status, item.Comment, item.NAReason, description)
		if err != nil {
			return nil, err
		}
		for _, ev := range item.EvidenceIDs {
			if checkErr := validateEvidenceLink(ctx, tx, owner, ev, item.RequirementID); checkErr != nil {
				return nil, checkErr
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO assessment_evidence(assessment_id,requirement_id,evidence_id) VALUES($1,$2,$3)`, id, item.RequirementID, ev); err != nil {
				return nil, err
			}
		}
	}
	return assessmentData(ctx, tx, id, owner)
}
func assessmentData(ctx context.Context, tx *sql.Tx, id, user string) (domain.Result, error) {
	a, err := jsonRow(ctx, tx, `SELECT to_jsonb(a) FROM assessments a WHERE id=$1 AND user_id=$2`, id, user)
	if err != nil {
		return nil, err
	}
	items, err := jsonRows(ctx, tx, `SELECT to_jsonb(i)||jsonb_build_object('evidence_ids',COALESCE((SELECT jsonb_agg(evidence_id ORDER BY evidence_id) FROM assessment_evidence WHERE assessment_id=i.assessment_id AND requirement_id=i.requirement_id),'[]')) FROM assessment_items i WHERE assessment_id=$1 ORDER BY requirement_id`, id)
	a["items"] = items
	return a, err
}
func growthContext(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	u, err := userMatrix(ctx, tx, c.Actor.UserID, c.Query.Get("user_matrix_id"))
	if err != nil {
		return nil, err
	}
	versionID, ok := u["matrix_version_id"].(string)
	if !ok {
		return nil, errors.New("invalid user matrix version")
	}
	v, err := versionData(ctx, tx, versionID)
	if err != nil {
		return nil, err
	}
	matrix, err := jsonRow(ctx, tx, `SELECT to_jsonb(m) FROM matrices m WHERE id=$1`, v["matrix_id"])
	if err != nil {
		return nil, err
	}
	user, err := jsonRow(ctx, tx, `SELECT jsonb_build_object('id',id,'name',name,'email',email) FROM users WHERE id=$1`, c.Actor.UserID)
	if err != nil {
		return nil, err
	}
	reqs, err := jsonRows(ctx, tx, `SELECT to_jsonb(r)||jsonb_build_object('skill_id',ms.skill_id,'level_key',l.key,'effective_description',COALESCE(o.description,r.description),'override_reason',o.reason) FROM requirements r JOIN matrix_skills ms ON ms.id=r.matrix_skill_id JOIN levels l ON l.id=r.level_id LEFT JOIN personal_overrides o ON o.requirement_id=r.id AND o.user_matrix_id=$2 WHERE r.matrix_version_id=$1 ORDER BY r.id`, u["matrix_version_id"], u["id"])
	if err != nil {
		return nil, err
	}
	facts, err := jsonRows(ctx, tx, `SELECT to_jsonb(f) FROM fact_types f ORDER BY key`)
	if err != nil {
		return nil, err
	}
	evidence, err := jsonRows(ctx, tx, `SELECT `+evidenceJSON+` FROM evidence e WHERE user_id=$1 AND EXISTS(SELECT 1 FROM evidence_matches m JOIN matrix_skills s ON s.skill_id=m.skill_id WHERE m.evidence_id=e.id AND s.matrix_version_id=$2) ORDER BY e.id DESC LIMIT 100`, c.Actor.UserID, u["matrix_version_id"])
	if err != nil {
		return nil, err
	}
	var current, target domain.Result
	levels, ok := v["levels"].([]domain.Result)
	if !ok {
		return nil, errors.New("invalid version levels")
	}
	for _, l := range levels {
		if l["id"] == u["current_level_id"] {
			current = l
		}
		if l["id"] == u["target_level_id"] {
			target = l
		}
	}
	signals := []domain.Result{}
	skills, ok := v["skills"].([]domain.Result)
	if !ok {
		return nil, errors.New("invalid version skills")
	}
	for _, s := range skills {
		signals = append(signals, domain.Result{"skill_id": s["skill_id"], "skill_key": s["skill_key"], "signals": s["signals"]})
	}
	return domain.Result{"user": user, "user_matrix": u, "matrix": matrix, "matrix_version": v, "current_level": current, "target_level": target, "competencies": v["groups"], "requirements": reqs, "skills": v["skills"], "signals": signals, "fact_types": facts, "evidence": evidence, "evidence_limit": 100}, nil
}
func readiness(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	u, err := userMatrix(ctx, tx, c.Actor.UserID, c.Query.Get("user_matrix_id"))
	if err != nil {
		return nil, err
	}
	var aid string
	err = tx.QueryRowContext(ctx, `SELECT id FROM assessments WHERE user_id=$1 AND user_matrix_id=$2 AND type='self' ORDER BY id DESC LIMIT 1`, c.Actor.UserID, u["id"]).Scan(&aid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	r, err := assessmentReadiness(ctx, tx, u["matrix_version_id"], u["target_level_id"], aid)
	if err != nil {
		return nil, err
	}
	groups := []domain.Result{}
	gaps := []domain.Result{}
	for _, g := range r.Groups {
		groups = append(groups, domain.Result{"group_id": g.GroupID, "percent": g.Percent})
	}
	for _, g := range r.Gaps {
		gaps = append(gaps, domain.Result{"requirement_id": g.RequirementID, "skill_id": g.SkillID, "group_id": g.GroupID, "score": g.Score, "required": g.Required, "critical": g.Critical})
	}
	if r.CriticalGaps == nil {
		r.CriticalGaps = []string{}
	}
	out := domain.Result{"percent": r.Percent, "ready": r.Ready, "critical_gaps": r.CriticalGaps, "gaps": gaps, "groups": groups, "assessment_id": nullable(aid), "user_matrix_id": u["id"], "policy": "weighted-v1"}
	if c.Operation == "getRadar" {
		currentRequirements, currentErr := radarRequirements(ctx, tx, u["matrix_version_id"], u["current_level_id"])
		if currentErr != nil {
			return nil, currentErr
		}
		targetRequirements, targetErr := radarRequirements(ctx, tx, u["matrix_version_id"], u["target_level_id"])
		if targetErr != nil {
			return nil, targetErr
		}
		percentByGroup := make(map[string]float64, len(r.Groups))
		for _, group := range r.Groups {
			percentByGroup[group.GroupID] = group.Percent
		}
		self := make([]domain.Result, 0, len(targetRequirements))
		for _, group := range targetRequirements {
			groupID, ok := group["group_id"].(string)
			if !ok {
				return nil, errors.New("invalid radar group ID")
			}
			self = append(self, domain.Result{"group_id": groupID, "name": group["name"], "percent": percentByGroup[groupID]})
		}
		out["manager_assessment_id"] = nil
		out["period"] = nil
		out["manager_author"] = nil
		var manager []domain.Result
		if aid != "" {
			var period string
			if periodErr := tx.QueryRowContext(ctx, `SELECT period FROM assessments WHERE id=$1`, aid).Scan(&period); periodErr != nil {
				return nil, periodErr
			}
			out["period"] = period
			mid, managerErr := latestManagerAssessment(ctx, tx, aid)
			if managerErr != nil {
				return nil, managerErr
			}
			if mid != "" {
				out["manager_assessment_id"] = mid
				out["manager_author"], err = jsonRow(ctx, tx, `SELECT jsonb_build_object('id',u.id,'email',u.email,'name',u.name) FROM users u JOIN assessments a ON a.assessor_id=u.id WHERE a.id=$1`, mid)
				if err != nil {
					return nil, err
				}
				mr, calcErr := assessmentReadiness(ctx, tx, u["matrix_version_id"], u["target_level_id"], mid)
				if calcErr != nil {
					return nil, calcErr
				}
				byGroup := make(map[string]float64, len(mr.Groups))
				for _, g := range mr.Groups {
					byGroup[g.GroupID] = g.Percent
				}
				manager = make([]domain.Result, 0, len(self))
				for _, g := range self {
					gid, ok := g["group_id"].(string)
					if !ok {
						return nil, errors.New("invalid radar group")
					}
					manager = append(manager, domain.Result{"group_id": gid, "name": g["name"], "percent": byGroup[gid]})
				}
			}
		}
		out["series"] = domain.Result{"self": self, "manager": manager, "current_requirements": currentRequirements, "target_requirements": targetRequirements}
	}
	return out, nil
}

type growthItem struct {
	RequirementID  string `json:"requirement_id"`
	Action, Status string
	EvidenceIDs    []string `json:"evidence_ids"`
}
type growthInput struct {
	UserMatrixID string `json:"user_matrix_id"`
	Deadline     string
	Items        []growthItem
}

func saveGrowthPlan(ctx context.Context, tx *sql.Tx, c domain.Command) (domain.Result, error) {
	in, err := decode[growthInput](c.Body)
	if err != nil {
		return nil, err
	}
	u, err := userMatrix(ctx, tx, c.Actor.UserID, in.UserMatrixID)
	if err != nil {
		return nil, err
	}
	deadline, err := parseDate(in.Deadline)
	if err != nil || deadline == nil {
		return nil, domain.Invalid("Deadline required as YYYY-MM-DD")
	}
	if len(in.Items) < 1 || len(in.Items) > 500 {
		return nil, domain.Invalid("Plan needs 1..500 items")
	}
	id := ID()
	if c.Operation == "updateGrowthPlan" {
		id = c.ID
		var uid string
		if checkErr := tx.QueryRowContext(ctx, `SELECT user_matrix_id FROM growth_plans WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, c.Actor.UserID).Scan(&uid); checkErr != nil {
			return nil, checkErr
		}
		if uid != in.UserMatrixID {
			return nil, domain.Invalid("Cannot reassign a growth plan")
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM growth_plan_items WHERE plan_id=$1`, id); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE growth_plans SET deadline=$2 WHERE id=$1`, id, deadline)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO growth_plans(id,user_id,user_matrix_id,target_level_id,deadline) VALUES($1,$2,$3,$4,$5)`, id, c.Actor.UserID, in.UserMatrixID, u["target_level_id"], deadline)
	}
	if err != nil {
		return nil, err
	}
	result := []domain.Result{}
	for _, item := range in.Items {
		if strings.TrimSpace(item.Action) == "" || len(item.Action) > 10000 || (item.Status != "planned" && item.Status != "in_progress" && item.Status != "done") {
			return nil, domain.Invalid("Invalid growth item")
		}
		if _, err = validateUserRequirement(ctx, tx, in.UserMatrixID, item.RequirementID); err != nil {
			return nil, err
		}
		var level string
		if checkErr := tx.QueryRowContext(ctx, `SELECT level_id FROM requirements WHERE id=$1`, item.RequirementID).Scan(&level); checkErr != nil {
			return nil, checkErr
		}
		if level != u["target_level_id"] {
			return nil, domain.Invalid("Growth items must address target-level requirements")
		}
		iid := ID()
		_, err = tx.ExecContext(ctx, `INSERT INTO growth_plan_items(id,plan_id,requirement_id,action,status) VALUES($1,$2,$3,$4,$5)`, iid, id, item.RequirementID, item.Action, item.Status)
		if err != nil {
			return nil, err
		}
		for _, e := range item.EvidenceIDs {
			if checkErr := validateEvidenceLink(ctx, tx, c.Actor.UserID, e, item.RequirementID); checkErr != nil {
				return nil, checkErr
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO growth_item_evidence(item_id,evidence_id) VALUES($1,$2)`, iid, e); err != nil {
				return nil, err
			}
		}
		result = append(result, domain.Result{"id": iid, "requirement_id": item.RequirementID, "action": item.Action, "status": item.Status, "evidence_ids": item.EvidenceIDs})
	}
	return domain.Result{"id": id, "user_matrix_id": in.UserMatrixID, "target_level_id": u["target_level_id"], "deadline": in.Deadline, "items": result}, nil
}

// radarRequirements includes every competency group, including groups with no
// active requirements at the selected level. Such groups have no expected score.
func radarRequirements(ctx context.Context, tx *sql.Tx, version, level any) ([]domain.Result, error) {
	return jsonRows(ctx, tx, `SELECT jsonb_build_object('group_id',g.id,'name',g.name,'requirements_count',count(r.id),'expected_score',CASE WHEN count(r.id)>0 THEN 4 ELSE 0 END) FROM competency_groups g LEFT JOIN matrix_skills m ON m.group_id=g.id AND m.matrix_version_id=g.matrix_version_id AND m.active LEFT JOIN requirements r ON r.matrix_skill_id=m.id AND r.level_id=$2 WHERE g.matrix_version_id=$1 GROUP BY g.id,g.name ORDER BY g.id`, version, level)
}

func assessmentReadiness(ctx context.Context, tx *sql.Tx, version, level any, aid string) (result domain.ReadinessResult, resultErr error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.id,m.skill_id,g.id,r.weight,m.weight,g.weight,(r.required OR m.required),r.critical,COALESCE(i.status='not_applicable',false),COALESCE(i.na_reason,''),COALESCE(i.score,0) FROM requirements r JOIN matrix_skills m ON m.id=r.matrix_skill_id JOIN competency_groups g ON g.id=m.group_id LEFT JOIN assessment_items i ON i.requirement_id=r.id AND i.assessment_id=$3 WHERE r.matrix_version_id=$1 AND r.level_id=$2 AND m.active ORDER BY r.id`, version, level, nullable(aid))
	if err != nil {
		return domain.ReadinessResult{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	progress := []domain.RequirementProgress{}
	for rows.Next() {
		var p domain.RequirementProgress
		if checkErr := rows.Scan(&p.RequirementID, &p.SkillID, &p.GroupID, &p.RequirementWeight, &p.SkillWeight, &p.GroupWeight, &p.Required, &p.Critical, &p.NotApplicable, &p.NAReason, &p.Score); checkErr != nil {
			return domain.ReadinessResult{}, checkErr
		}
		progress = append(progress, p)
	}
	if checkErr := rows.Err(); checkErr != nil {
		return domain.ReadinessResult{}, checkErr
	}
	return domain.CalculateReadiness(progress)
}
