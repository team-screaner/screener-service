-- +goose Up
CREATE TABLE matrix_reviewers (
 user_matrix_id uuid PRIMARY KEY REFERENCES user_matrices(id),
 reviewer_id uuid NOT NULL REFERENCES users(id),
 assigned_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX matrix_reviewers_reviewer ON matrix_reviewers(reviewer_id,user_matrix_id);
ALTER TABLE assessments ADD COLUMN assessor_id uuid REFERENCES users(id);
ALTER TABLE assessments ADD COLUMN reviewed_assessment_id uuid REFERENCES assessments(id);
ALTER TABLE assessments ADD CONSTRAINT manager_assessment_attribution CHECK (
 (type='manager' AND assessor_id IS NOT NULL AND assessor_id<>user_id AND reviewed_assessment_id IS NOT NULL)
 OR (type<>'manager' AND reviewed_assessment_id IS NULL)
);
CREATE INDEX assessments_reviewed ON assessments(reviewed_assessment_id,id DESC) WHERE type='manager';

-- +goose Down
DROP INDEX assessments_reviewed;
ALTER TABLE assessments DROP CONSTRAINT manager_assessment_attribution;
ALTER TABLE assessments DROP COLUMN reviewed_assessment_id;
ALTER TABLE assessments DROP COLUMN assessor_id;
DROP TABLE matrix_reviewers;
