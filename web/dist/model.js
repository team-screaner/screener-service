export const escapeHTML = (value) =>
  String(value ?? "").replace(
    /[&<>"']/g,
    (char) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        char
      ],
  );
export const percent = (value) =>
  Math.min(100, Math.max(0, Number(value) || 0));
export function safeURL(value) {
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) ? url.href : "";
  } catch {
    return "";
  }
}
export function targetRequirements(context) {
  if (!context) return [];
  return context.requirements
    .filter((req) => req.level_id === context.user_matrix.target_level_id)
    .flatMap((req) => {
      const skill = context.skills.find(
        (s) => s.id === req.matrix_skill_id || s.skill_id === req.skill_id,
      );
      if (skill?.active === false) return [];
      return [
        {
          ...req,
          text: req.effective_description || req.description,
          skill_name: skill?.name || "Навык",
          skill_key: skill?.skill_key || "",
          group_id: skill?.group_id || req.group_id,
          required: req.required || skill?.required,
        },
      ];
    });
}
export const acceptedFor = (evidence, id) =>
  evidence.filter(
    (e) =>
      ["accepted", "manager_confirmed"].includes(e.status) &&
      e.matches?.some((m) => m.requirement_id === id),
  );
export function assessmentItems(requirements, draft, evidence) {
  return requirements.map((req) => {
    const item = draft[req.id] || {};
    const status = item.status || "assessed";
    if (status === "not_applicable" && !item.na_reason?.trim())
      throw new Error(`Укажите причину N/A для «${req.skill_name}».`);
    const score = status === "not_applicable" ? 0 : Number(item.score || 0);
    if (!Number.isFinite(score) || score < 0 || score > 4)
      throw new Error("Оценка должна быть от 0 до 4.");
    const ids =
      status === "not_applicable" ? [] : [...new Set(item.evidence_ids || [])];
    const accepted = new Set(acceptedFor(evidence, req.id).map((e) => e.id));
    if ((score > 0 && ids.length === 0) || ids.some((id) => !accepted.has(id)))
      throw new Error(`Выберите принятый факт для «${req.skill_name}».`);
    return {
      requirement_id: req.id,
      status,
      score,
      na_reason: item.na_reason || "",
      comment: item.comment || "",
      evidence_ids: ids,
    };
  });
}
export const quarter = () =>
  `${new Date().getFullYear()}-Q${Math.floor(new Date().getMonth() / 3) + 1}`;
export const dateLabel = (value) =>
  value
    ? new Intl.DateTimeFormat("ru", {
        day: "numeric",
        month: "short",
        year: "numeric",
      }).format(new Date(value))
    : "—";
export function growthPayload(plan, itemID, status) {
  return {
    user_matrix_id: plan.user_matrix_id,
    deadline: plan.deadline.slice(0, 10),
    items: plan.items.map((item) => ({
      requirement_id: item.requirement_id,
      action: item.action,
      status: item.id === itemID ? status : item.status,
      evidence_ids: item.evidence_ids || [],
    })),
  };
}
