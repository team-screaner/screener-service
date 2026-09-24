import { Api } from "./api.js";
import {
  escapeHTML as h,
  percent,
  safeURL,
  targetRequirements,
  acceptedFor,
  assessmentItems,
  quarter,
  dateLabel,
  growthPayload,
} from "./model.js";

const app = document.querySelector("#app");
const dialog = document.querySelector("#modal");
const state = {
  user: null,
  matrices: [],
  assignments: [],
  evidence: [],
  plans: [],
  tokens: [],
  facts: [],
  context: null,
  radar: null,
  assessment: null,
  draft: {},
  dirty: false,
  active: "",
  view: "overview",
  filter: "all",
  search: "",
  busy: false,
  authMode: "login",
  error: "",
};
const api = new Api({
  onUnauthorized: () => {
    state.user = null;
    state.context = null;
    state.dirty = false;
    dialog.close();
    render();
  },
});
const navItems = [
  ["overview", "Обзор", "grid"],
  ["matrix", "Моя матрица", "matrix"],
  ["evidence", "Факты", "folder"],
  ["assessment", "Самооценка", "check"],
  ["plan", "План развития", "flag"],
  ["catalog", "Каталог матриц", "layers"],
];
const titles = {
  overview: "Мой рост",
  matrix: "Моя матрица",
  evidence: "Факты и результаты",
  assessment: "Самооценка",
  plan: "План развития",
  catalog: "Каталог матриц",
  settings: "Агенты и доступ",
};
const statuses = {
  accepted: "Принят",
  suggested: "На проверке",
  rejected: "Отклонён",
  manager_confirmed: "Подтверждён",
  planned: "Запланировано",
  in_progress: "В работе",
  done: "Готово",
  published: "Опубликована",
  draft: "Черновик",
};
const iconPaths = {
  grid: '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
  matrix:
    '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M9 4v16M15 4v16"/>',
  folder:
    '<path d="M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z"/><path d="M8 12h8M8 16h5"/>',
  check:
    '<rect x="4" y="3" width="16" height="18" rx="3"/><path d="m8 12 3 3 5-6"/>',
  flag: '<path d="M5 21V3m0 1c5-5 9 5 15 0v10c-6 5-10-5-15 0"/>',
  layers:
    '<path d="m12 3 10 5-10 5L2 8l10-5zm-10 9 10 5 10-5M2 16l10 5 10-5"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  arrow: '<path d="M5 12h14m-6-6 6 6-6 6"/>',
  logout: '<path d="M9 3H4v18h5M10 12h11m-5-5 5 5-5 5"/>',
  settings:
    '<circle cx="8" cy="15" r="5"/><path d="m12 11 9-9m-4 4 3 3m-6 0 3 3"/>',
  download: '<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  search: '<circle cx="10" cy="10" r="6"/><path d="m15 15 6 6"/>',
  close: '<path d="m6 6 12 12M6 18 18 6"/>',
  link: '<path d="m9 15 6-6M8 17l-2 2a4 4 0 0 1-6-6l5-5m11-1 2-2a4 4 0 0 1 6 6l-5 5"/>',
};
const icon = (name, cls = "") =>
  `<svg class="icon ${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.65" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${iconPaths[name] || iconPaths.grid}</svg>`;
const badge = (status) =>
  `<span class="badge ${h(status)}">${h(statuses[status] || status)}</span>`;
const button = (text, action, cls = "primary", data = "") =>
  `<button type="button" class="button ${cls}" data-action="${action}" ${data}>${text}</button>`;
const viewLink = (text, view, cls = "button secondary") =>
  `<a class="${cls}" href="#/${view}">${text}</a>`;
const number = (value) =>
  new Intl.NumberFormat("ru", { maximumFractionDigits: 1 }).format(value || 0);
const selected = (value, current) => (value === current ? " selected" : "");
const requirements = () => targetRequirements(state.context);
const requirement = (id) => requirements().find((r) => r.id === id);
const progress = (value) =>
  `<progress max="100" value="${percent(value)}" aria-label="Прогресс ${number(percent(value))}%"></progress>`;
const empty = (title, text, action = "") =>
  `<div class="empty"><span class="empty-icon">${icon("layers")}</span><h2>${title}</h2><p>${text}</p>${action}</div>`;
function toast(text) {
  const node = document.querySelector("#toast");
  node.textContent = text;
  node.classList.add("visible");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => node.classList.remove("visible"), 4500);
}
function showError(error, form) {
  let node = form?.querySelector(".form-error");
  if (!node && dialog.open) {
    node = dialog.querySelector(".form-error");
    if (!node) {
      node = document.createElement("p");
      node.className = "form-error";
      node.setAttribute("role", "alert");
      node.tabIndex = -1;
      dialog.querySelector(".modal-body").append(node);
    }
  }
  node ||= document.querySelector("#global-error");
  if (node) {
    node.textContent = error.message || String(error);
    node.hidden = false;
    node.focus();
  } else toast(error.message || String(error));
}
function field(label, name, type = "text", extra = "") {
  return `<label class="field">${label}<input name="${name}" type="${type}" ${extra}></label>`;
}
function formEnd(label) {
  return `<p class="form-error" role="alert" tabindex="-1" hidden></p><div class="form-actions"><button class="button primary" type="submit">${label}</button></div>`;
}
function openModal(title, content, wide = false) {
  dialog.className = wide ? "wide" : "";
  dialog.innerHTML = `<div class="modal-head"><h2 id="modal-title">${h(title)}</h2>${button(icon("close"), "close", "icon-button", 'aria-label="Закрыть"')}</div><div class="modal-body">${content}</div>`;
  if (!dialog.open) dialog.showModal();
}
function noMatrix() {
  return empty(
    "Выберите свою траекторию",
    "Начните с матрицы компетенций: укажите текущий и целевой уровень, чтобы видеть требования и отслеживать прогресс.",
    viewLink("Выбрать матрицу " + icon("arrow"), "catalog", "button primary"),
  );
}
function authView() {
  const register = state.authMode === "register";
  return `<main id="main" class="auth-layout"><section class="auth-story"><a class="brand" href="#/overview"><span class="brand-mark">s.</span>Screener</a><div><p class="eyebrow">РАЗВИТИЕ ЧЕРЕЗ РЕЗУЛЬТАТЫ</p><h1>Следующий уровень.<br>Понятный путь.</h1><p class="auth-description">Требования к роли, результаты вашей работы и конкретные шаги для роста — в одном месте.</p><ol class="auth-steps"><li><b>01</b><span>Выберите матрицу компетенций</span></li><li><b>02</b><span>Подтвердите навыки фактами</span></li><li><b>03</b><span>Составьте план развития</span></li></ol></div><span class="auth-footer">Ваш профессиональный рост — в ваших руках</span></section><section class="auth-main"><div class="auth-form"><span class="label-pill">ЛИЧНОЕ ПРОСТРАНСТВО</span><h2>${register ? "Начнём вашу траекторию" : "С возвращением"}</h2><p class="muted">${register ? "Создайте аккаунт, чтобы сохранить свою матрицу и результаты." : "Войдите, чтобы продолжить работу над своим развитием."}</p><form id="auth-form">${register ? field("Как к вам обращаться", "name", "text", 'required maxlength="200" autocomplete="name"') : ""}${field("Электронная почта", "email", "email", 'required autocomplete="username" placeholder="you@company.com"')}${field("Пароль", "password", "password", `required minlength="12" maxlength="72" autocomplete="${register ? "new-password" : "current-password"}"`)}${register ? '<p class="field-note">От 12 символов; максимум 72 байта.</p>' : ""}${formEnd(register ? "Создать аккаунт " + icon("arrow") : "Войти " + icon("arrow"))}</form><p class="auth-switch">${register ? "Уже есть аккаунт?" : "Первый раз здесь?"} ${button(register ? "Войти" : "Создать аккаунт", "auth-toggle", "text-button")}</p><p class="privacy-note">Факты и самооценки доступны только вам.</p></div></section></main>`;
}
function render() {
  const focus = document.activeElement?.id;
  const position = document.activeElement?.selectionStart;
  if (!state.user) {
    app.innerHTML = authView();
    return;
  }
  state.view = location.hash.slice(2).split("?")[0] || "overview";
  if (!titles[state.view]) state.view = "overview";
  const ctx = state.context;
  app.innerHTML = `<div class="shell"><aside class="sidebar"><a class="brand" href="#/overview"><span class="brand-mark">s.</span>Screener</a><p class="nav-label">МОЁ ПРОСТРАНСТВО</p><nav aria-label="Основная навигация">${navItems.map(([id, label, ico]) => `<a href="#/${id}" class="nav-item ${state.view === id ? "active" : ""}" ${state.view === id ? 'aria-current="page"' : ""}>${icon(ico)}<span>${label}</span>${id === "evidence" && state.evidence.some((e) => e.status === "suggested") ? `<span class="nav-count">${state.evidence.filter((e) => e.status === "suggested").length}</span>` : ""}</a>`).join("")}</nav><div class="sidebar-bottom"><a class="nav-item ${state.view === "settings" ? "active" : ""}" href="#/settings">${icon("settings")}Агенты и доступ</a><a class="nav-item" href="/docs/" target="_blank" rel="noopener">${icon("link")}Документация API</a><div class="user-block"><span class="avatar">${h(state.user.name?.slice(0, 1).toUpperCase() || "Я")}</span><div><strong>${h(state.user.name)}</strong><small>Личный аккаунт</small></div>${button(icon("logout"), "logout", "icon-button", 'aria-label="Выйти"')}</div></div></aside><div class="workspace"><header class="topbar"><span>Моё пространство <span class="slash">/</span> <strong>${h(titles[state.view])}</strong><span class="topbar-right">${ctx ? `<span class="role-chip">${h(ctx.current_level.name)} ${icon("arrow")} ${h(ctx.target_level.name)}</span>` : ""}<span class="avatar small">${h(state.user.name?.slice(0, 1).toUpperCase() || "Я")}</span></span></header><main id="main" tabindex="-1"><div id="global-error" class="error-banner" role="alert" tabindex="-1" ${state.error ? "" : "hidden"}>${h(state.error)}</div><div class="page-heading"><div><p class="eyebrow">${h(ctx?.matrix.name || "ВАША ТРАЕКТОРИЯ")}</p><h1>${h(titles[state.view])}</h1><p class="muted">${subtitle()}</p></div><div class="page-actions">${state.assignments.length > 1 ? `<label class="sr-only" for="assignment-switch">Траектория</label><select id="assignment-switch">${state.assignments.map((a, i) => `<option value="${h(a.id)}"${selected(a.id, state.active)}>${h(a.id === state.active && ctx ? ctx.matrix.name : `Траектория ${i + 1}`)}</option>`).join("")}</select>` : ""}${pageAction()}</div></div><div id="page-content">${pageContent()}</div><footer class="workspace-footer"><span>Screener <span class="muted">/</span> Личное развитие</span><span>Оценки подкреплены результатами</span></footer></main></div></div>`;
  if (focus) {
    const el = document.getElementById(focus);
    if (el && ["INPUT", "TEXTAREA"].includes(el.tagName)) {
      el.focus();
      if (position != null && ["text", "search"].includes(el.type))
        el.setSelectionRange(position, position);
    }
  }
}
function subtitle() {
  return {
    overview: "Ваша точка сейчас и следующий шаг.",
    matrix: "Требования целевого уровня и подтверждение каждого навыка.",
    evidence: "Результаты работы, на которые можно опереться.",
    assessment: "Оцените выполнение требований и свяжите оценки с фактами.",
    plan: "Превратите зоны роста в конкретные действия.",
    catalog: "Выберите основу для своей траектории или загрузите свою матрицу.",
    settings: "Подключите агента для сбора фактов с ограниченными правами.",
  }[state.view];
}
function pageAction() {
  if (state.view === "catalog")
    return button(icon("plus") + "Импорт XLSX", "import", "secondary");
  if (state.view === "settings")
    return button(icon("plus") + "Создать токен", "token");
  if (!state.context && state.view !== "evidence") return "";
  if (state.view === "overview" || state.view === "evidence")
    return button(icon("plus") + "Добавить факт", "evidence");
  if (state.view === "matrix")
    return button(icon("download") + "Экспорт XLSX", "export", "secondary");
  if (state.view === "plan")
    return button(icon("plus") + "Добавить действие", "plan");
  return "";
}
function pageContent() {
  if (state.view === "catalog") return catalogView();
  if (state.view === "evidence") return evidenceView();
  if (state.view === "settings") return settingsView();
  if (!state.context) return noMatrix();
  return {
    overview: overviewView,
    matrix: matrixView,
    assessment: assessmentView,
    plan: planView,
  }[state.view]();
}
function overviewView() {
  const r = state.radar,
    ctx = state.context,
    reqs = requirements();
  const accepted = state.evidence.filter((e) =>
    ["accepted", "manager_confirmed"].includes(e.status),
  );
  const related = accepted.filter((e) =>
    e.matches?.some((m) => reqs.some((req) => req.id === m.requirement_id)),
  );
  const actions = state.plans.flatMap((p) => p.items),
    done = actions.filter((i) => i.status === "done").length;
  const completed =
    state.assessment?.items.filter(
      (i) =>
        i.score === 4 &&
        i.status === "assessed" &&
        reqs.some((req) => req.id === i.requirement_id),
    ).length || 0;
  const nextGaps = [...(r?.gaps || [])]
    .sort(
      (a, b) =>
        Number(b.critical) - Number(a.critical) ||
        Number(b.required) - Number(a.required),
    )
    .slice(0, 3);
  return `<section class="journey-banner"><div><span class="eyebrow">ТЕКУЩАЯ ЦЕЛЬ</span><h2>${h(ctx.current_level.name)} <span>→</span> ${h(ctx.target_level.name)}</h2><p>${h(ctx.matrix.name)} <span>·</span> Версия ${ctx.matrix_version.number}</p></div><div class="journey-action">${viewLink("Открыть матрицу " + icon("arrow"), "matrix", "button light")}</div></section><section class="metrics" aria-label="Ключевые показатели"><article class="metric"><span>Готовность к цели</span><strong>${number(r?.percent)}<small>%</small></strong>${progress(r?.percent)}<p>${r?.ready ? "Критерии готовности выполнены" : !r?.assessment_id ? "Начните с самооценки" : "Есть требования для проработки"}</p></article><article class="metric"><span>Требования выполнены</span><strong>${completed}<small> / ${reqs.length}</small></strong><p>Оценка 4 из 4, подтверждённая фактами</p></article><article class="metric"><span>Принятые факты</span><strong>${related.length}</strong><p>Связаны с целевыми требованиями</p></article><article class="metric"><span>Действия завершены</span><strong>${done}<small> / ${actions.length}</small></strong><p>В вашем плане развития</p></article></section><div class="dashboard-grid"><section class="panel"><div class="panel-head"><div><h2>Профиль компетенций</h2><p>Самооценка относительно целевого уровня</p></div><span class="legend"><i></i>Самооценка</span></div>${radarChart()}<div class="radar-note">Цель — 100% по каждой группе. Оценка руководителя пока не предусмотрена.</div></section><section class="panel"><div class="panel-head"><div><h2>Следующий шаг</h2><p>Начните с того, что влияет на готовность</p></div></div>${
    !r?.assessment_id
      ? `<div class="next-step"><span class="step-number">01</span><h3>Зафиксируйте точку старта</h3><p>Добавьте факты и оцените требования. Это поможет выбрать приоритеты для роста.</p>${viewLink("Перейти к самооценке " + icon("arrow"), "assessment", "button primary")}</div>`
      : nextGaps.length
        ? `<div class="gap-list">${nextGaps
            .map((gap) => {
              const req = requirement(gap.requirement_id);
              return `<article class="gap-item"><div><h3>${h(req?.skill_name || "Требование")}</h3><p>${h(req?.text || "")}</p><span class="badge ${gap.critical ? "critical" : "neutral"}">${gap.critical ? "Критичное" : gap.required ? "Обязательное" : "Зона роста"}</span></div>${button(icon("plus"), "plan", "icon-button", `data-req="${h(gap.requirement_id)}" aria-label="Добавить в план"`)}</article>`;
            })
            .join("")}</div>`
        : empty(
            "Цель достигнута",
            "Все применимые требования выполнены. Продолжайте собирать результаты или выберите новую траекторию.",
            viewLink("Открыть каталог", "catalog"),
          )
  }</section></div><section class="panel activity-panel"><div class="panel-head"><div><h2>Последние результаты</h2><p>Факты вашей работы</p></div>${viewLink("Все факты " + icon("arrow"), "evidence", "text-link")}</div>${state.evidence.length ? evidenceRows(state.evidence.slice().reverse().slice(0, 3)) : `<div class="inline-empty">${icon("folder")}<p>Здесь появятся ваши результаты: задачи, проекты, улучшения.</p>${button("Добавить первый факт", "evidence", "secondary")}</div>`}</section>`;
}
function radarChart() {
  const groups = state.radar?.series?.self || [];
  if (groups.length < 3)
    return `<div class="group-progress">${groups.map((g) => `<div><span>${h(g.name)} <b>${number(g.percent)}%</b></span>${progress(g.percent)}</div>`).join("")}</div>`;
  const n = groups.length,
    cx = 180,
    cy = 160,
    radius = 108;
  const point = (i, scale = 1) => [
    cx + Math.sin((i * 2 * Math.PI) / n) * radius * scale,
    cy - Math.cos((i * 2 * Math.PI) / n) * radius * scale,
  ];
  const points = (scale) =>
    groups
      .map((g, i) =>
        point(i, typeof scale === "function" ? scale(g) : scale).join(","),
      )
      .join(" ");
  return `<div class="radar"><svg viewBox="0 0 360 330" role="img" aria-label="Самооценка по группам компетенций"><title>${h(groups.map((g) => `${g.name}: ${number(g.percent)}%`).join("; "))}</title>${[0.25, 0.5, 0.75, 1].map((scale) => `<polygon points="${points(scale)}" class="radar-grid"/>`).join("")}${groups
    .map((g, i) => {
      const [x, y] = point(i);
      return `<line x1="${cx}" y1="${cy}" x2="${x}" y2="${y}" class="radar-axis"/>`;
    })
    .join(
      "",
    )}<polygon points="${points((g) => percent(g.percent) / 100)}" class="radar-value"/>${groups
    .map((g, i) => {
      const [x, y] = point(i, 1.22);
      const label = String(i + 1);
      return `<text x="${x}" y="${y}" text-anchor="middle" dominant-baseline="middle">${h(label)}</text>`;
    })
    .join(
      "",
    )}</svg><div class="radar-groups">${groups.map((g, i) => `<span>${i + 1}. ${h(g.name)}<b>${number(g.percent)}%</b></span>`).join("")}</div></div>`;
}
function matrixView() {
  const reqs = requirements().filter((r) =>
    (r.skill_name + " " + r.text)
      .toLowerCase()
      .includes(state.search.toLowerCase()),
  );
  return `<div class="toolbar"><label class="search">${icon("search")}<input id="search" type="search" placeholder="Найти навык или требование" value="${h(state.search)}" aria-label="Поиск по матрице"></label><span class="muted">${reqs.length} требований · ${h(state.context.target_level.name)}</span></div><section class="panel table-panel"><div class="table-wrap"><table><thead><tr><th>Навык / требование</th><th>Приоритет</th><th>Самооценка</th><th>Факты</th><th><span class="sr-only">Действия</span></th></tr></thead><tbody>${reqs
    .map((req) => {
      const item = state.assessment?.items.find(
        (i) => i.requirement_id === req.id,
      );
      const ev = acceptedFor(state.evidence, req.id);
      return `<tr><td><strong>${h(req.skill_name)}</strong><p>${h(req.text)}</p>${req.override_reason ? '<span class="field-note">Личная формулировка</span>' : ""}</td><td>${req.critical ? '<span class="badge critical">Критичное</span>' : req.required ? '<span class="badge required">Обязательное</span>' : '<span class="muted">Обычное</span>'}</td><td><span class="score-chip">${item?.status === "not_applicable" ? "N/A" : item ? number(item.score) + " / 4" : "—"}</span></td><td><span class="fact-count">${icon("folder")}${ev.length}</span></td><td>${button(icon("plus"), "evidence", "icon-button", `data-req="${h(req.id)}" aria-label="Добавить факт для ${h(req.skill_name)}"`)}</td></tr>`;
    })
    .join(
      "",
    )}</tbody></table>${!reqs.length ? empty("Ничего не найдено", "Попробуйте другой запрос.") : ""}</div></section><p class="field-note">Готовность рассчитывается по последней сохранённой самооценке. Принятие факта само по себе не меняет оценку.</p>`;
}
function evidenceRows(items) {
  return `<div class="evidence-list">${items
    .map(
      (e) =>
        `<article class="evidence-row"><span class="file-icon">${icon("folder")}</span><div class="evidence-main"><div class="evidence-title"><h3>${h(e.title)}</h3>${badge(e.status)}</div><p>${h(e.description)}</p><div class="evidence-meta"><span>${h(state.facts.find((f) => f.key === e.fact_type)?.name || e.fact_type || "Факт")}</span><span>${dateLabel(e.period_to || e.created_at)}</span>${safeURL(e.source_url) ? `<a href="${h(safeURL(e.source_url))}" target="_blank" rel="noopener noreferrer">Открыть источник ↗</a>` : ""}</div>${
          e.matches?.length
            ? `<div class="evidence-tags">${e.matches
                .slice(0, 3)
                .map(
                  (m) =>
                    `<span>${h(requirement(m.requirement_id)?.skill_name || m.skill_key || "Навык")}</span>`,
                )
                .join("")}</div>`
            : '<small class="muted">Пока не связан с требованием</small>'
        }</div>${e.status === "suggested" ? `<div class="review-actions">${button("Принять", "review", "secondary", `data-id="${h(e.id)}" data-status="accepted"`)}${button("Отклонить", "review", "text-button", `data-id="${h(e.id)}" data-status="rejected"`)}</div>` : ""}</article>`,
    )
    .join("")}</div>`;
}
function evidenceView() {
  const items = state.evidence
    .filter(
      (e) =>
        (state.filter === "all" || e.status === state.filter) &&
        (e.title + " " + e.description)
          .toLowerCase()
          .includes(state.search.toLowerCase()),
    )
    .reverse();
  return `<div class="toolbar"><div class="tabs" role="group" aria-label="Статус фактов">${[
    ["all", "Все"],
    ["accepted", "Принятые"],
    ["suggested", "На проверке"],
    ["rejected", "Отклонённые"],
  ]
    .map(([id, label]) =>
      button(
        `${label} <small>${state.evidence.filter((e) => id === "all" || e.status === id).length}</small>`,
        "filter",
        state.filter === id ? "tab active" : "tab",
        `data-filter="${id}" aria-pressed="${state.filter === id}"`,
      ),
    )
    .join(
      "",
    )}</div><label class="search">${icon("search")}<input type="search" id="search" placeholder="Поиск фактов" aria-label="Поиск фактов" value="${h(state.search)}"></label></div><section class="panel">${items.length ? evidenceRows(items) : empty("Пока нет фактов", state.search || state.filter !== "all" ? "Измените поиск или фильтр." : "Добавьте завершённую задачу, проект или улучшение. Свяжите результат с требованием вашей матрицы.", button("Добавить факт", "evidence"))}</section>`;
}
function assessmentView() {
  const reqs = requirements();
  return `<div class="notice">${icon("check")}<div>Оценка — от 0 до 4. Для оценки выше нуля нужен принятый факт.<br><span class="muted">0 — пока нет подтверждения; 4 — требование выполнено полностью. N/A исключает требование из расчёта и требует причины.</span></div></div><form id="assessment-form"><div class="assessment-top"><label class="field">Период<input name="period" value="${h(state.period || quarter())}" required maxlength="100"></label><div>${state.assessment ? `<p class="muted">Последний снимок: ${h(state.assessment.period)} · ${dateLabel(state.assessment.created_at)}</p>` : '<p class="muted">Это будет ваша первая самооценка.</p>'}<span id="draft-status" class="badge neutral">${state.dirty ? "Есть несохранённые изменения" : "Новый снимок"}</span></div></div><div class="assessment-list">${reqs
    .map((req) => {
      const item = state.draft[req.id] || {};
      const ev = acceptedFor(state.evidence, req.id);
      const na = item.status === "not_applicable";
      return `<article class="assessment-card" data-requirement="${h(req.id)}"><div class="assessment-heading"><div><span class="eyebrow">${h(state.context.competencies.find((g) => g.id === req.group_id)?.name || "КОМПЕТЕНЦИЯ")}</span><h3>${h(req.skill_name)}</h3></div>${req.critical ? '<span class="badge critical">Критичное</span>' : req.required ? '<span class="badge required">Обязательное</span>' : ""}</div><p class="requirement-text">${h(req.text)}</p><div class="assessment-controls"><label class="field">Оценка<select data-draft="score" data-req="${h(req.id)}" ${na ? "disabled" : ""}>${[
        ...new Set([0, 1, 2, 3, 4, Number(item.score || 0)]),
      ]
        .sort((a, b) => a - b)
        .map(
          (n) =>
            `<option value="${n}"${selected(n, Number(item.score || 0))}>${n} / 4${n === 0 ? " — нет подтверждения" : n === 4 ? " — выполнено" : ""}</option>`,
        )
        .join(
          "",
        )}</select></label><label class="check-label"><input type="checkbox" data-draft="na" data-req="${h(req.id)}" ${na ? "checked" : ""}>Не применимо (N/A)</label></div>${na ? `<label class="field">Почему не применимо<textarea data-draft="na_reason" data-req="${h(req.id)}" required>${h(item.na_reason)}</textarea></label>` : `<div class="evidence-picker"><strong>Подтверждающие факты</strong>${ev.length ? ev.map((e) => `<label class="check-label"><input type="checkbox" data-draft="evidence" data-req="${h(req.id)}" value="${h(e.id)}" ${(item.evidence_ids || []).includes(e.id) ? "checked" : ""}>${h(e.title)}</label>`).join("") : `<p class="muted">Для этого требования пока нет принятого факта.</p>`}${button(icon("plus") + "Добавить факт", "evidence", "text-button", `data-req="${h(req.id)}"`)}</div>`}<label class="field">Комментарий <span class="optional">необязательно</span><textarea data-draft="comment" data-req="${h(req.id)}" rows="2">${h(item.comment)}</textarea></label></article>`;
    })
    .join(
      "",
    )}</div><div class="save-bar"><span>Сохранится новый неизменяемый снимок.</span><button class="button primary" type="submit">Сохранить самооценку ${icon("check")}</button></div><p class="form-error" role="alert" tabindex="-1" hidden></p></form>`;
}
function planView() {
  if (!state.plans.length)
    return empty(
      "Дайте цели конкретный следующий шаг",
      "Выберите требование, опишите действие и поставьте срок. Завершённые задачи помогут собрать новые подтверждающие факты.",
      button("Добавить первое действие", "plan"),
    );
  return `<div class="plan-list">${state.plans
    .slice()
    .reverse()
    .map(
      (plan) =>
        `<section class="panel"><div class="panel-head"><div><span class="eyebrow">ПЛАН РАЗВИТИЯ</span><h2>До ${dateLabel(plan.deadline)}</h2></div><span class="badge neutral">${plan.items.filter((i) => i.status === "done").length} / ${plan.items.length} выполнено</span></div>${plan.items.map((item) => `<article class="plan-row"><span class="plan-state ${h(item.status)}">${icon(item.status === "done" ? "check" : "flag")}</span><div class="plan-main"><h3>${h(item.action)}</h3><p>${h(requirement(item.requirement_id)?.skill_name || "Требование")}</p><p class="muted">${h(requirement(item.requirement_id)?.text || "")}</p></div><label class="sr-only" for="plan-${h(item.id)}">Статус действия</label><select id="plan-${h(item.id)}" data-plan="${h(plan.id)}" data-item="${h(item.id)}">${["planned", "in_progress", "done"].map((s) => `<option value="${s}"${selected(s, item.status)}>${statuses[s]}</option>`).join("")}</select>${button(icon("plus") + "Факт", "evidence", "secondary", `data-req="${h(item.requirement_id)}"`)}</article>`).join("")}</section>`,
    )
    .join("")}</div>`;
}
function catalogView() {
  const matrices = state.matrices.filter((m) =>
    (m.name + " " + m.description)
      .toLowerCase()
      .includes(state.search.toLowerCase()),
  );
  return `<div class="toolbar"><label class="search wide-search">${icon("search")}<input type="search" id="search" placeholder="Найти профессию или матрицу" aria-label="Поиск матриц" value="${h(state.search)}"></label><span class="muted">${matrices.length} матриц</span></div><div class="catalog-grid">${matrices.map((matrix, i) => `<article class="matrix-card"><div class="matrix-card-top"><span class="catalog-icon tone-${i % 4}">${icon("layers")}</span>${badge(matrix.status || "published")}</div><span class="eyebrow">${matrix.scope === "system" ? "СИСТЕМНЫЙ ШАБЛОН" : matrix.scope === "personal" ? "ЛИЧНАЯ МАТРИЦА" : "МАТРИЦА ОРГАНИЗАЦИИ"}</span><h2>${h(matrix.name)}</h2><p>${h(matrix.description || "Матрица компетенций с требованиями по уровням.")}</p><div class="matrix-card-footer">${button("Посмотреть " + icon("arrow"), "matrix-preview", "secondary", `data-id="${h(matrix.id)}"`)}</div></article>`).join("")}</div>${!matrices.length ? empty("Матрицы не найдены", "Измените запрос или импортируйте свою матрицу.") : ""}`;
}
function settingsView() {
  return `<div class="notice">${icon("settings")}<div>Агент может читать матрицу и предлагать факты. Самооценку сохраняете только вы.<br><span class="muted">Выдавайте только необходимые права. Созданный секрет будет показан один раз.</span></div></div><section class="panel">${state.tokens.length ? `<div class="token-list">${state.tokens.map((t) => `<article class="token-row"><div><h3>${h(t.name)}</h3><p class="muted">До ${dateLabel(t.expires_at)}</p><div class="evidence-tags">${t.scopes.map((scope) => `<span>${h(scope)}</span>`).join("")}</div></div>${t.revoked_at ? '<span class="badge neutral">Отозван</span>' : button("Отозвать", "revoke", "secondary", `data-id="${h(t.id)}"`)}</article>`).join("")}</div>` : empty("Нет подключённых агентов", "Создайте токен, чтобы агент мог присылать факты вашей работы.", button("Создать токен", "token"))}</section>`;
}

let loadVersion = 0;
async function loadWorkspace() {
  const generation = ++loadVersion;
  const [user, matrices, assignments, evidence, facts, tokens] =
    await Promise.all([
      api.get("/me"),
      api.all("/matrices"),
      api.all("/user-matrices"),
      api.all("/evidence"),
      api.all("/fact-types"),
      api.all("/tokens"),
    ]);
  if (generation !== loadVersion) return;
  Object.assign(state, {
    user,
    matrices,
    assignments,
    evidence,
    facts,
    tokens,
    error: "",
  });
  if (!assignments.some((a) => a.id === state.active))
    state.active = assignments.at(-1)?.id || "";
  if (state.active) await loadContext(generation);
  else
    Object.assign(state, {
      context: null,
      radar: null,
      plans: [],
      assessment: null,
      draft: {},
    });
  if (generation === loadVersion) render();
}
async function loadContext(generation = loadVersion) {
  const query = { user_matrix_id: state.active };
  const [context, radar, plans] = await Promise.all([
    api.get("/me/growth-context", query),
    api.get("/me/radar", query),
    api.all("/growth-plans", query),
  ]);
  const assessment = radar.assessment_id
    ? await api.get(`/assessments/${radar.assessment_id}`)
    : null;
  if (generation !== loadVersion) return;
  Object.assign(state, { context, radar, plans, assessment });
  if (!state.dirty)
    state.draft = Object.fromEntries(
      (assessment?.items || []).map((i) => [
        i.requirement_id,
        { ...i, evidence_ids: [...(i.evidence_ids || [])] },
      ]),
    );
}
async function busy(action, form) {
  if (state.busy) return;
  state.busy = true;
  const controls = form
    ? "button, input, textarea, select"
    : "button, #assignment-switch, select[data-plan]";
  const buttons = [...(form || document).querySelectorAll(controls)].filter(
    (b) => !b.disabled,
  );
  buttons.forEach((b) => {
    b.disabled = true;
  });
  const errorNode = form?.querySelector(".form-error");
  if (errorNode) errorNode.hidden = true;
  try {
    await action();
  } catch (err) {
    showError(err, form?.isConnected ? form : null);
  } finally {
    state.busy = false;
    buttons.forEach((b) => {
      b.disabled = false;
    });
  }
}
function evidenceModal(reqID = "") {
  const reqs = requirements();
  openModal(
    "Добавить результат работы",
    `<form id="evidence-form">${field("Что было сделано", "title", "text", 'required maxlength="500" placeholder="Например, ускорил обработку запросов"')}<label class="field">Результат и ваш вклад<textarea name="description" required rows="4" placeholder="Что изменилось, как вы это проверили и какую часть работы выполнили лично"></textarea></label><label class="field">Тип факта<select name="fact_type" required>${state.facts.map((f) => `<option value="${h(f.key)}">${h(f.name)}</option>`).join("")}</select></label><label class="field">Требование матрицы<select name="requirement_id"><option value="">Без привязки к требованию</option>${reqs.map((r) => `<option value="${h(r.id)}"${selected(r.id, reqID)}>${h(r.skill_name + " — " + r.text.slice(0, 100))}</option>`).join("")}</select></label>${!reqs.length ? '<p class="field-note">Выберите траекторию в каталоге, чтобы связывать факты с требованиями.</p>' : ""}${field("Ссылка на результат", "source_url", "url", 'placeholder="https://…"')}<div class="form-grid">${field("Начало периода", "period_from", "date")}${field("Конец периода", "period_to", "date")}</div><p class="field-note">Ваш ручной факт будет принят сразу. Агентские предложения требуют отдельного подтверждения.</p>${formEnd("Сохранить факт")}</form>`,
  );
}
function planModal(reqID = "") {
  if (!state.context) {
    location.hash = "/catalog";
    return;
  }
  const reqs = requirements();
  const deadline = new Date();
  deadline.setDate(deadline.getDate() + 30);
  openModal(
    "Следующий шаг развития",
    `<form id="plan-form"><label class="field">Требование<select name="requirement_id" required>${reqs.map((r) => `<option value="${h(r.id)}"${selected(r.id, reqID)}>${h(r.skill_name + " — " + r.text.slice(0, 100))}</option>`).join("")}</select></label><label class="field">Что вы сделаете<textarea name="action" required rows="4" placeholder="Конкретное действие и ожидаемый результат"></textarea></label>${field("Срок", "deadline", "date", `required value="${deadline.toISOString().slice(0, 10)}"`)}${formEnd("Добавить в план")}</form>`,
  );
}
async function matrixPreview(id) {
  const matrix = await api.get(`/matrices/${id}`);
  const versions = matrix.versions || [];
  const summary = versions.find((v) => v.status === "published") || versions[0];
  if (!summary) throw new Error("У матрицы пока нет доступных версий.");
  const version = await api.get(`/matrix-versions/${summary.id}`);
  state.preview = { matrix, version };
  showMatrixPreview();
}
function showMatrixPreview() {
  const { matrix, version } = state.preview;
  const options = version.levels
    .map((l) => `<option value="${h(l.id)}">${h(l.name)}</option>`)
    .join("");
  openModal(
    matrix.name,
    `<div class="preview-meta">${badge(version.status)}<span>Версия ${version.number}</span><span>${version.skills.length} навыков</span><span>${version.levels.length} уровня</span></div><p class="muted">${h(matrix.description)}</p><details><summary>Посмотреть требования (${version.requirements.length})</summary><div class="preview-requirements">${version.requirements.map((r) => `<p><strong>${h(version.skills.find((s) => s.skill_id === r.skill_id)?.name)} · ${h(version.levels.find((l) => l.id === r.level_id)?.name)}</strong><br>${h(r.description)}</p>`).join("")}</div></details>${version.status === "published" ? `<form id="assign-form"><div class="form-grid"><label class="field">Текущий уровень<select name="current_level_id">${options}</select></label><label class="field">Целевой уровень<select name="target_level_id">${version.levels.map((l, i) => `<option value="${h(l.id)}" ${i === Math.min(1, version.levels.length - 1) ? "selected" : ""}>${h(l.name)}</option>`).join("")}</select></label></div>${formEnd("Выбрать эту траекторию")}</form>` : `<div class="notice">После публикации версия станет неизменяемой. Проверьте требования перед публикацией.</div>${button("Опубликовать матрицу", "publish", "primary", `data-id="${h(version.id)}"`)}`}<hr><p class="field-note">Системные шаблоны можно адаптировать: экспортируйте XLSX, измените требования и импортируйте как личную матрицу.</p>${button(icon("download") + "Скачать XLSX", "export-version", "secondary", `data-id="${h(version.id)}"`)}`,
    true,
  );
}
function importModal() {
  state.importInput = null;
  openModal(
    "Импорт матрицы из XLSX",
    `<form id="import-form">${field("Название матрицы", "name", "text", 'required maxlength="200"')}${field("Файл XLSX", "file", "file", 'required accept=".xlsx"')}<p class="field-note">До 10 МБ. Столбцы: Category, Skill, затем названия уровней. Сначала проверим содержимое, затем создадим личный черновик.</p>${field("Лист", "sheet", "text", 'placeholder="Первый лист по умолчанию"')}<details><summary>Свои названия столбцов</summary>${field("Столбец категории", "category", "text", 'placeholder="Category"')}${field("Столбец навыка", "skill", "text", 'placeholder="Skill"')}${field("Столбцы уровней через запятую", "levels", "text", 'placeholder="Junior, Middle, Senior"')}</details>${formEnd("Проверить файл")}</form>`,
    true,
  );
}
function tokenModal() {
  openModal(
    "Токен для агента",
    `<form id="token-form">${field("Название", "name", "text", 'required placeholder="Например, мой агент GitHub"')}${field("Срок действия, дней", "days", "number", 'required min="1" max="90" value="30"')}<fieldset><legend>Разрешения</legend>${[
      ["matrix:read", "Читать матрицы"],
      ["evidence:read", "Читать факты"],
      ["evidence:write", "Предлагать факты"],
      ["assessment:read", "Читать самооценки"],
    ]
      .map(
        ([value, label]) =>
          `<label class="check-label"><input type="checkbox" name="scope" value="${value}" ${value === "matrix:read" ? "checked" : ""}>${label}</label>`,
      )
      .join("")}</fieldset>${formEnd("Создать токен")}</form>`,
  );
}
async function download(path, filename) {
  const blob = await api.request(path, { binary: true });
  const url = URL.createObjectURL(blob),
    link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

async function submit(form, data) {
  const value = (name) => String(data.get(name) || "").trim();
  if (form.id === "auth-form") {
    const password = String(data.get("password") || "");
    if (new TextEncoder().encode(password).length > 72)
      throw new Error("Пароль должен занимать не больше 72 байт.");
    const body = { email: value("email"), password };
    if (state.authMode === "register") body.name = value("name");
    const session = await api.command(`/auth/${state.authMode}`, body);
    api.setToken(session.token);
    state.authMode = "login";
    state.dirty = false;
    state.active = "";
    state.context = null;
    await loadWorkspace();
    toast("Вы вошли в своё пространство");
  } else if (form.id === "assign-form") {
    const { version } = state.preview;
    const current = value("current_level_id"),
      target = value("target_level_id");
    if (
      version.levels.findIndex((l) => l.id === target) <
      version.levels.findIndex((l) => l.id === current)
    )
      throw new Error("Целевой уровень не должен быть ниже текущего.");
    const assignment = await api.command("/user-matrices", {
      matrix_version_id: version.id,
      current_level_id: current,
      target_level_id: target,
    });
    state.active = assignment.id;
    state.dirty = false;
    dialog.close();
    await loadWorkspace();
    location.hash = "/overview";
    render();
    toast("Траектория выбрана");
  } else if (form.id === "evidence-form") {
    const req = requirement(value("requirement_id"));
    const payload = {
      title: value("title"),
      description: value("description"),
      fact_type: value("fact_type"),
      source: "manual",
      source_url: value("source_url"),
      matches: req
        ? [
            {
              skill_key: req.skill_key,
              requirement_id: req.id,
              confidence: 1,
              reason: "Связь указана автором факта",
            },
          ]
        : [],
    };
    if (value("period_from")) payload.period_from = value("period_from");
    if (value("period_to")) payload.period_to = value("period_to");
    if (
      payload.period_from &&
      payload.period_to &&
      payload.period_from > payload.period_to
    )
      throw new Error("Конец периода должен быть не раньше начала.");
    await api.command("/evidence", payload);
    dialog.close();
    await loadWorkspace();
    toast("Факт сохранён и принят");
  } else if (form.id === "assessment-form") {
    const items = assessmentItems(requirements(), state.draft, state.evidence);
    await api.command("/assessments", {
      user_matrix_id: state.active,
      period: value("period"),
      type: "self",
      items,
    });
    state.dirty = false;
    await loadWorkspace();
    location.hash = "/overview";
    render();
    toast("Самооценка сохранена. Прогресс обновлён.");
  } else if (form.id === "plan-form") {
    await api.command("/growth-plans", {
      user_matrix_id: state.active,
      deadline: value("deadline"),
      items: [
        {
          requirement_id: value("requirement_id"),
          action: value("action"),
          status: "planned",
          evidence_ids: [],
        },
      ],
    });
    dialog.close();
    await loadWorkspace();
    toast("Действие добавлено в план");
  } else if (form.id === "token-form") {
    const scopes = data.getAll("scope");
    if (!scopes.length) throw new Error("Выберите хотя бы одно разрешение.");
    const token = await api.command("/tokens", {
      name: value("name"),
      scopes,
      expires_in_days: Number(value("days")),
    });
    openModal(
      "Сохраните токен",
      `<p>Скопируйте его сейчас и передайте своему агенту. После закрытия окна секрет больше не показывается.</p><label class="field">Токен<textarea readonly rows="3" id="new-token">${h(token.token)}</textarea></label>${button("Скопировать токен", "copy-token", "primary")}<p class="field-note">Никому не передавайте этот токен, кроме выбранного агента.</p>`,
    );
    await loadWorkspace();
  } else if (form.id === "import-form") {
    const file = data.get("file");
    if (!file?.size || file.size > 10 * 1024 * 1024)
      throw new Error("Выберите XLSX-файл размером до 10 МБ.");
    const bytes = new Uint8Array(await file.arrayBuffer());
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += 8192)
      binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
    const payload = {
      file_base64: btoa(binary),
      name: value("name"),
      confirm: false,
    };
    if (value("sheet")) payload.sheet = value("sheet");
    const mapping = {};
    if (value("category")) mapping.category = value("category");
    if (value("skill")) mapping.skill = value("skill");
    if (value("levels"))
      mapping.levels = value("levels")
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
    if (Object.keys(mapping).length) payload.mapping = mapping;
    const result = await api.command("/import/xlsx", payload);
    state.importInput = payload;
    const preview = result.preview;
    openModal(
      "Проверьте импорт",
      `<div class="notice">Файл проверен. После подтверждения появится личный черновик матрицы.</div><h3>${h(preview.Name || preview.name)}</h3><p>${(preview.Rows || preview.rows).length} навыков · ${(preview.Levels || preview.levels).map(h).join(" → ")}</p><div class="preview-requirements">${(
        preview.Rows || preview.rows
      )
        .slice(0, 20)
        .map(
          (r) =>
            `<p><strong>${h(r.Skill || r.skill)}</strong><br>${h(r.Category || r.category)}</p>`,
        )
        .join(
          "",
        )}</div>${button("Создать черновик", "confirm-import", "primary")}`,
      true,
    );
  }
}

document.addEventListener("submit", (event) => {
  if (event.target.matches("form")) {
    event.preventDefault();
    const data = new FormData(event.target);
    busy(() => submit(event.target, data), event.target);
  }
});
document.addEventListener("click", (event) => {
  const trigger = event.target.closest("[data-action]");
  if (!trigger || state.busy) return;
  const { action, id, req } = trigger.dataset;
  if (action === "close") {
    dialog.close();
    return;
  }
  if (action === "auth-toggle") {
    state.authMode = state.authMode === "login" ? "register" : "login";
    render();
    return;
  }
  if (action === "filter") {
    state.filter = trigger.dataset.filter;
    render();
    return;
  }
  if (action === "evidence") {
    evidenceModal(req);
    return;
  }
  if (action === "plan") {
    planModal(req);
    return;
  }
  if (action === "token") {
    tokenModal();
    return;
  }
  if (action === "import") {
    importModal();
    return;
  }
  busy(async () => {
    if (action === "logout") {
      if (state.dirty && !confirm("Выйти и потерять несохранённую самооценку?"))
        return;
      await api.command("/auth/logout");
      api.setToken("");
      ++loadVersion;
      Object.assign(state, {
        user: null,
        context: null,
        dirty: false,
        active: "",
        draft: {},
        evidence: [],
        tokens: [],
      });
      dialog.close();
      render();
    } else if (action === "matrix-preview") await matrixPreview(id);
    else if (action === "review") {
      const evidence = state.evidence.find((e) => e.id === id);
      await api.command(
        `/evidence/${id}`,
        { status: trigger.dataset.status, version: evidence.version },
        "PATCH",
      );
      await loadWorkspace();
      toast(
        trigger.dataset.status === "accepted" ? "Факт принят" : "Факт отклонён",
      );
    } else if (action === "export")
      await download(
        `/export/xlsx?user_matrix_id=${encodeURIComponent(state.active)}`,
        "screener-growth.xlsx",
      );
    else if (action === "export-version")
      await download(
        `/export/xlsx?matrix_version_id=${encodeURIComponent(id)}`,
        "screener-matrix.xlsx",
      );
    else if (action === "publish") {
      await api.command(`/matrix-versions/${id}/publish`);
      await loadWorkspace();
      await matrixPreview(state.preview.matrix.id);
      toast("Матрица опубликована");
    } else if (action === "confirm-import") {
      const matrix = await api.command("/import/xlsx", {
        ...state.importInput,
        confirm: true,
      });
      state.importInput = null;
      await loadWorkspace();
      await matrixPreview(matrix.id);
      toast("Личный черновик создан");
    } else if (action === "copy-token") {
      const input = document.querySelector("#new-token");
      try {
        await navigator.clipboard.writeText(input.value);
        toast("Токен скопирован");
      } catch {
        input.select();
        toast("Нажмите Ctrl/Cmd+C, чтобы скопировать токен");
      }
    } else if (action === "revoke") {
      await api.command(`/tokens/${id}`, undefined, "DELETE");
      await loadWorkspace();
      toast("Токен отозван");
    }
  });
});
document.addEventListener("input", (event) => {
  const target = event.target;
  if (target.id === "search") {
    state.search = target.value;
    render();
  }
  if (target.name === "period") {
    state.period = target.value;
    state.dirty = true;
  }
  if (
    target.dataset.draft &&
    !["na", "score", "evidence"].includes(target.dataset.draft)
  )
    updateDraft(target);
});
function updateDraft(target) {
  const id = target.dataset.req,
    kind = target.dataset.draft;
  const item = (state.draft[id] ||= {
    score: 0,
    status: "assessed",
    evidence_ids: [],
  });
  if (kind === "na")
    item.status = target.checked ? "not_applicable" : "assessed";
  else if (kind === "score") item.score = Number(target.value);
  else if (kind === "evidence")
    item.evidence_ids = target.checked
      ? [...new Set([...(item.evidence_ids || []), target.value])]
      : (item.evidence_ids || []).filter((id) => id !== target.value);
  else item[kind] = target.value;
  state.dirty = true;
  const status = document.querySelector("#draft-status");
  if (status) status.textContent = "Есть несохранённые изменения";
}
document.addEventListener("change", (event) => {
  const target = event.target;
  if (target.dataset.draft) {
    updateDraft(target);
    if (target.dataset.draft === "na") render();
  }
  if (target.id === "assignment-switch") {
    if (
      state.dirty &&
      !confirm("Переключить траекторию и потерять несохранённую самооценку?")
    ) {
      target.value = state.active;
      return;
    }
    const previous = state.active;
    state.dirty = false;
    state.active = target.value;
    busy(async () => {
      try {
        await loadContext(++loadVersion);
        render();
      } catch (error) {
        state.active = previous;
        target.value = previous;
        throw error;
      }
    });
  }
  if (target.dataset.plan) {
    const plan = state.plans.find((p) => p.id === target.dataset.plan);
    busy(async () => {
      try {
        await api.command(
          `/growth-plans/${plan.id}`,
          growthPayload(plan, target.dataset.item, target.value),
          "PUT",
        );
        await loadWorkspace();
        toast("Статус обновлён");
      } catch (error) {
        target.value = plan.items.find(
          (i) => i.id === target.dataset.item,
        ).status;
        throw error;
      }
    });
  }
});
window.addEventListener("hashchange", () => {
  state.search = "";
  state.filter = "all";
  render();
  document.querySelector("#main")?.focus({ preventScroll: true });
  window.scrollTo(0, 0);
});
window.addEventListener("beforeunload", (event) => {
  if (state.dirty) {
    event.preventDefault();
    event.returnValue = "";
  }
});
dialog.addEventListener("cancel", (event) => {
  if (state.busy) event.preventDefault();
});
if (api.token) {
  loadWorkspace().catch((error) => {
    state.user = null;
    render();
    showError(error, document.querySelector("#auth-form"));
  });
} else render();
