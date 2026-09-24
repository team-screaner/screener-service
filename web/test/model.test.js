import test from "node:test";
import assert from "node:assert/strict";
import {
  escapeHTML,
  targetRequirements,
  assessmentItems,
  acceptedFor,
  safeURL,
} from "../dist/model.js";

const context = {
  user_matrix: { target_level_id: "target" },
  skills: [
    {
      id: "ms",
      skill_id: "skill",
      name: "Go",
      active: true,
      group_id: "group",
    },
  ],
  requirements: [
    {
      id: "r",
      matrix_skill_id: "ms",
      skill_id: "skill",
      level_id: "target",
      description: "Original",
      effective_description: "Personal",
    },
    { id: "old", matrix_skill_id: "ms", level_id: "current" },
  ],
};
const evidence = [
  { id: "ok", status: "accepted", matches: [{ requirement_id: "r" }] },
  { id: "suggested", status: "suggested", matches: [{ requirement_id: "r" }] },
  { id: "other", status: "accepted", matches: [{ requirement_id: "old" }] },
];

test("target requirements retain the personal text and skill names", () => {
  assert.equal(targetRequirements(context).length, 1);
  assert.equal(targetRequirements(context)[0].text, "Personal");
  assert.equal(targetRequirements(context)[0].skill_name, "Go");
});
test("only accepted evidence matched to the exact requirement can support a score", () => {
  assert.deepEqual(
    acceptedFor(evidence, "r").map((x) => x.id),
    ["ok"],
  );
  assert.throws(
    () =>
      assessmentItems(
        targetRequirements(context),
        { r: { score: 2 } },
        evidence,
      ),
    /факт/,
  );
  assert.throws(
    () =>
      assessmentItems(
        targetRequirements(context),
        { r: { score: 2, evidence_ids: ["other"] } },
        evidence,
      ),
    /факт/,
  );
  assert.equal(
    assessmentItems(
      targetRequirements(context),
      { r: { score: 2, evidence_ids: ["ok"] } },
      evidence,
    )[0].score,
    2,
  );
});
test("N/A requires a reason and always has zero score", () => {
  assert.throws(
    () =>
      assessmentItems(
        targetRequirements(context),
        { r: { status: "not_applicable" } },
        evidence,
      ),
    /причин/,
  );
  const [item] = assessmentItems(
    targetRequirements(context),
    { r: { status: "not_applicable", score: 4, na_reason: "Not used here" } },
    evidence,
  );
  assert.equal(item.score, 0);
  assert.deepEqual(item.evidence_ids, []);
});
test("user-provided text and links cannot become executable markup", () => {
  assert.equal(
    escapeHTML('<img src=x onerror="1">'),
    "&lt;img src=x onerror=&quot;1&quot;&gt;",
  );
  assert.equal(safeURL("javascript:alert(1)"), "");
  assert.equal(safeURL("https://example.com/pr/1"), "https://example.com/pr/1");
});

test('a replacement manager starts an independent draft and never inherits another author’s ratings', async () => {
  const { managerDraft } = await import('../dist/model.js');
  const assessment = {assessor_id:'old-manager',items:[{requirement_id:'r',score:4,evidence_ids:['e']}]};
  assert.deepEqual(managerDraft(assessment, 'new-manager'), {});
  const own = managerDraft(assessment, 'old-manager');
  assert.equal(own.r.score,4);
  own.r.evidence_ids.push('new');
  assert.deepEqual(assessment.items[0].evidence_ids,['e']);
});
