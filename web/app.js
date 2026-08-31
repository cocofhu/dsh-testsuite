const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => [...document.querySelectorAll(sel)];

let page = "envs";
let images = [];
let envs = [];
let presets = [];
let imageBusy = false;
/** 环境行内操作进行中：跳过会替换按钮 DOM 的轮询重绘 */
let envBusy = false;
/** 预设/镜像删除等列表操作进行中 */
let presetBusy = false;
let envsLoaded = false;
let presetsLoaded = false;
let imagesLoaded = false;

const ACT_LABELS = {
  renew: { idle: "续期 6h", busy: "续期中…" },
  stop: { idle: "停止", busy: "停止中…" },
  restart: { idle: "重启", busy: "重启中…" },
  start: { idle: "启动", busy: "启动中…" },
  destroy: { idle: "销毁", busy: "销毁中…" },
  logs: { idle: "日志", busy: "加载中…" },
};

// 记住上一次成功创建环境时提交的预装插件(localStorage key)
const LAST_PLUGINS_KEY = "dsh.lastPlugins";

// 读取上次预装插件记录;localStorage 不可用或脏数据时静默按无记录处理
function readLastPlugins() {
  try {
    const raw = window.localStorage.getItem(LAST_PLUGINS_KEY);
    if (!raw) return null;
    const val = JSON.parse(raw);
    if (!Array.isArray(val)) {
      // 脏数据:顺手清除,按无记录处理
      window.localStorage.removeItem(LAST_PLUGINS_KEY);
      return null;
    }
    return val.map((s) => String(s).trim()).filter(Boolean);
  } catch {
    return null;
  }
}

// 写入/清除上次预装插件记录;失败时静默降级,不影响主流程
function saveLastPlugins(list) {
  try {
    if (Array.isArray(list) && list.length) {
      window.localStorage.setItem(LAST_PLUGINS_KEY, JSON.stringify(list));
    } else {
      window.localStorage.removeItem(LAST_PLUGINS_KEY);
    }
  } catch {
    // localStorage 不可用(隐私模式/配额满等):忽略
  }
}

function showBanner(msg) {
  const el = $("#banner");
  if (!msg) {
    el.hidden = true;
    el.textContent = "";
    return;
  }
  el.hidden = false;
  el.textContent = msg;
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
    ...opts,
  });
  if (res.status === 204) return null;
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = { error: text };
  }
  if (!res.ok) {
    throw new Error((data && data.error) || res.statusText);
  }
  return data;
}

function statusPill(status) {
  const map = {
    running: ["运行中", "ok"],
    creating: ["创建中", "warn"],
    stopped: ["已停止", ""],
    error: ["错误", "err"],
    ready: ["本机有", "ok"],
    missing: ["本机没有", "err"],
    healthy: ["Healthy", "ok"],
    starting: ["Starting", "warn"],
  };
  const [label, cls] = map[status] || [status || "—", "warn"];
  return `<span class="pill ${cls}">${escapeHtml(label)}</span>`;
}

function healthPill(health) {
  if (!health) return '<span class="muted">—</span>';
  return statusPill(health);
}

function escapeHtml(s) {
  return String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function pluginsCell(list) {
  if (!list || !list.length) return '<span class="muted">—</span>';
  return escapeHtml(list.join(", "));
}

function formatTTL(destroyAt) {
  if (!destroyAt) return '<span class="muted">—</span>';
  const ms = new Date(destroyAt).getTime() - Date.now();
  const title = escapeHtml(new Date(destroyAt).toLocaleString());
  if (Number.isNaN(ms)) return '<span class="muted">—</span>';
  if (ms <= 0) return `<span class="pill err" title="${title}">已过期</span>`;
  const totalSec = Math.floor(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  let label;
  if (h > 0) label = `${h}h ${m}m`;
  else if (m > 0) label = `${m}m`;
  else label = `${totalSec}s`;
  const cls = ms < 30 * 60 * 1000 ? "warn" : "ok";
  return `<span class="pill ${cls}" title="${title}">${label}</span>`;
}

function setHTML(el, html) {
  if (el.dataset.html === html) return false;
  el.dataset.html = html;
  el.innerHTML = html;
  return true;
}

/** 点击当下同步：被点按钮转圈+进行中文案，并禁用同行其他变更按钮 */
function setButtonBusy(btn, busyLabel, { siblings = true } = {}) {
  if (!btn) return;
  if (!btn.dataset.idleLabel) btn.dataset.idleLabel = btn.textContent.trim();
  btn.disabled = true;
  btn.classList.add("loading");
  btn.innerHTML = `<span class="btn-spin" aria-hidden="true"></span>${escapeHtml(busyLabel)}`;
  if (siblings) {
    const row = btn.closest("tr");
    if (row) {
      row.querySelectorAll("button[data-act], button[data-img-pull], button[data-img-del], button[data-preset-del], button[data-preset-edit]").forEach((b) => {
        if (b !== btn && b.dataset.act !== "open") b.disabled = true;
      });
    }
  }
}

function clearButtonBusy(btn) {
  if (!btn) return;
  const idle = btn.dataset.idleLabel || "";
  btn.classList.remove("loading");
  btn.disabled = false;
  btn.textContent = idle;
  delete btn.dataset.idleLabel;
}

function setSubmitBusy(submit, cancel, busyLabel) {
  if (submit) {
    if (!submit.dataset.idleLabel) submit.dataset.idleLabel = submit.textContent.trim();
    submit.disabled = true;
    submit.classList.add("loading");
    submit.innerHTML = `<span class="btn-spin" aria-hidden="true"></span>${escapeHtml(busyLabel)}`;
  }
  if (cancel) cancel.disabled = true;
}

function clearSubmitBusy(submit, cancel, idleFallback) {
  if (submit) {
    const idle = submit.dataset.idleLabel || idleFallback || "";
    submit.classList.remove("loading");
    submit.disabled = false;
    submit.textContent = idle;
    delete submit.dataset.idleLabel;
  }
  if (cancel) cancel.disabled = false;
}

function showOverlay(host, label) {
  if (!host) return;
  let overlay = host.querySelector(":scope > .view-overlay, :scope > .dialog-overlay");
  const cls = host.classList.contains("dialog") ? "dialog-overlay" : "view-overlay";
  if (!overlay) {
    overlay = document.createElement("div");
    overlay.className = cls;
    overlay.setAttribute("role", "status");
    host.appendChild(overlay);
  }
  overlay.innerHTML = `<span class="btn-spin" aria-hidden="true"></span>${escapeHtml(label)}`;
}

function hideOverlay(host) {
  if (!host) return;
  host.querySelectorAll(":scope > .view-overlay, :scope > .dialog-overlay").forEach((el) => el.remove());
}

function skeletonRows(cols, n = 2) {
  const widths = ["w70", "w40", "w55", "w40", "w55", "w70", "w40", "w90", "w55"];
  return Array.from({ length: n }, () => {
    const cells = Array.from({ length: cols }, (_, i) => {
      const cls = i === 0 ? "td-name" : "";
      return `<td class="${cls}"><span class="skel ${widths[i % widths.length]}"></span></td>`;
    }).join("");
    return `<tr class="skel-row">${cells}</tr>`;
  }).join("");
}

function showSkeleton(which) {
  if (which === "envs" && !envsLoaded) {
    setHTML($("#env-rows"), skeletonRows(9));
  }
  if (which === "presets" && !presetsLoaded) {
    setHTML($("#preset-rows"), skeletonRows(5));
  }
  if (which === "images" && !imagesLoaded) {
    setHTML($("#image-rows"), skeletonRows(4));
  }
}

function renderEnvs() {
  const tb = $("#env-rows");
  if (!envs.length) {
    setHTML(tb, `<tr><td colspan="9" class="muted">还没有环境。先到「镜像版本」登记已构建的 runtime 镜像，再创建环境。</td></tr>`);
    return;
  }
  const html = envs
    .map((e) => {
      const running = e.status === "running";
      const stopped = e.status === "stopped" || e.status === "error";
      const healthy = running && e.health === "healthy";
      return `<tr data-env-id="${escapeHtml(e.id)}">
        <td class="td-name">${escapeHtml(e.name)}<div class="muted">${escapeHtml(e.id)}</div></td>
        <td data-label="状态">${statusPill(e.status)}${e.error ? `<div class="muted">${escapeHtml(e.error)}</div>` : ""}</td>
        <td data-label="Health">${healthPill(e.health)}</td>
        <td data-label="TTL">${formatTTL(e.destroyAt)}</td>
        <td data-label="dsh 版本">${escapeHtml(e.dshVersion)}</td>
        <td data-label="Provider">${escapeHtml(e.provider)}</td>
        <td data-label="Model">${escapeHtml(e.model)}</td>
        <td data-label="插件">${pluginsCell(e.plugins)}</td>
        <td class="actions">
          <button class="btn" data-act="open" data-id="${e.id}" ${healthy && e.openURL ? "" : "disabled"} ${running && !healthy ? 'title="等待 Health"' : ""}>打开</button>
          <button class="btn" data-act="logs" data-id="${e.id}">日志</button>
          <button class="btn" data-act="renew" data-id="${e.id}" title="到期时间再延 6 小时">续期 6h</button>
          ${running ? `<button class="btn" data-act="stop" data-id="${e.id}">停止</button>` : ""}
          ${running ? `<button class="btn" data-act="restart" data-id="${e.id}">重启</button>` : ""}
          ${stopped ? `<button class="btn" data-act="start" data-id="${e.id}">启动</button>` : ""}
          <button class="btn" data-act="destroy" data-id="${e.id}">销毁</button>
        </td>
      </tr>`;
    })
    .join("");
  setHTML(tb, html);
}

function renderImages() {
  const tb = $("#image-rows");
  if (!images.length) {
    setHTML(tb, `<tr><td colspan="4" class="muted">还没有登记镜像。点击右上角选择版本，或手动填写本机已构建的镜像名。</td></tr>`);
    return;
  }
  const html = images
    .map(
      (im) => `<tr>
        <td class="td-name">${escapeHtml(im.version)}</td>
        <td data-label="镜像">${escapeHtml(im.ref)}</td>
        <td data-label="本机">${im.present ? statusPill("ready") : statusPill("missing")}${im.error ? `<div class="muted">${escapeHtml(im.error)}</div>` : ""}</td>
        <td class="actions">
          ${im.present ? "" : `<button class="btn" data-img-pull="${escapeHtml(im.version)}">拉取</button>`}
          <button class="btn" data-img-del="${escapeHtml(im.version)}">删除</button>
        </td>
      </tr>`
    )
    .join("");
  setHTML(tb, html);
}

function fillVersionSelect() {
  const sel = document.querySelector('#form-create [name="dshVersion"]');
  const prev = sel.value;
  const html = images.length
    ? images.map((im) => `<option value="${escapeHtml(im.version)}">${escapeHtml(im.version)}${im.present ? "" : "（本机没有）"}</option>`).join("")
    : `<option value="">（请先登记镜像）</option>`;
  if (!setHTML(sel, html)) return;
  if ([...sel.options].some((o) => o.value === prev)) sel.value = prev;
}

let providerOptions = [];

async function loadProviders() {
  const data = await api("/api/providers");
  providerOptions = data.providers || [];
  fillProviderSelect($("#form-create"));
  fillProviderSelect($("#form-preset"));
  syncPresetForm();
}

function fillProviderSelect(form) {
  const sel = form.querySelector('[name="provider"]');
  if (!sel) return;
  const prev = sel.value;
  const groups = { official: [], catalog: [], custom: [] };
  for (const p of providerOptions) {
    (groups[p.kind] || groups.custom).push(p);
  }
  const opt = (p) => `<option value="${escapeHtml(p.id)}" data-kind="${escapeHtml(p.kind)}">${escapeHtml(p.label)}</option>`;
  sel.innerHTML =
    groups.official.map(opt).join("") +
    (groups.catalog.length
      ? `<optgroup label="pi-ai 目录">${groups.catalog.map(opt).join("")}</optgroup>`
      : "") +
    groups.custom.map(opt).join("");
  if ([...sel.options].some((o) => o.value === prev)) sel.value = prev;
  syncProviderFields(form);
}

function fillPresetSelect() {
  const sel = document.querySelector('#form-create [name="modelPreset"]');
  const prev = sel.value;
  sel.innerHTML =
    presets.map((p) => `<option value="${escapeHtml(p.id)}">${escapeHtml(p.name)}</option>`).join("") +
    `<option value="manual">手动填写</option>`;
  if ([...sel.options].some((o) => o.value === prev)) sel.value = prev;
  else if (presets.length) sel.value = presets[0].id;
  else sel.value = "manual";
}

function selectedPreset() {
  const id = document.querySelector('#form-create [name="modelPreset"]').value;
  if (!id || id === "manual") return null;
  return presets.find((p) => p.id === id) || null;
}

function syncPresetForm() {
  const manual = !selectedPreset();
  const box = $("#manual-model");
  box.hidden = !manual;
  box.querySelectorAll("input, select").forEach((el) => {
    el.disabled = !manual;
  });
  if (manual) {
    const key = box.querySelector('[name="apiKey"]');
    if (key) key.required = true;
    syncProviderFields($("#form-create"));
  } else {
    box.querySelectorAll("input, select").forEach((el) => {
      el.required = false;
    });
  }
}

function providerKind(form) {
  const sel = form.querySelector('[name="provider"]');
  const opt = sel && sel.selectedOptions[0];
  return (opt && opt.dataset.kind) || "official";
}

function selectedProvider(form) {
  const id = form.querySelector('[name="provider"]').value;
  return providerOptions.find((p) => p.id === id);
}

function modelLabel(m) {
  if (m.name && m.name !== m.id) return `${m.name} (${m.id})`;
  return m.id;
}

function showField(form, name, on) {
  const el = form.querySelector(`[data-field="${name}"]`);
  if (el) el.hidden = !on;
}

function fillModels(form) {
  const kind = providerKind(form);
  const models = (selectedProvider(form) && selectedProvider(form).models) || [];
  const modelSel = form.querySelector('[name="model"]');
  const modelId = form.querySelector('[name="modelId"]');
  const useSelect = kind !== "custom" && models.length > 0;
  showField(form, "modelSelect", useSelect);
  showField(form, "modelId", !useSelect);
  if (modelSel) {
    modelSel.disabled = !useSelect;
    modelSel.required = useSelect;
  }
  if (modelId) {
    modelId.required = !useSelect;
    modelId.disabled = useSelect;
  }
  if (useSelect && modelSel) {
    const prev = modelSel.value;
    modelSel.innerHTML = models
      .map((m) => `<option value="${escapeHtml(m.id)}">${escapeHtml(modelLabel(m))}</option>`)
      .join("");
    if ([...modelSel.options].some((o) => o.value === prev)) modelSel.value = prev;
  }
}

function syncProviderFields(form) {
  const custom = providerKind(form) === "custom";
  showField(form, "providerId", custom);
  showField(form, "baseURL", custom);
  showField(form, "api", custom);
  const providerId = form.querySelector('[name="providerId"]');
  const base = form.querySelector('[name="baseURL"]');
  if (providerId) providerId.required = custom;
  if (base) base.required = custom;
  fillModels(form);
}

function presetBody(form) {
  const fd = new FormData(form);
  const kind = providerKind(form);
  const body = {
    name: fd.get("name"),
    provider: kind === "custom" ? fd.get("providerId") : fd.get("provider"),
    model: kind === "custom" || !fd.get("model") ? fd.get("modelId") : fd.get("model"),
    apiKey: fd.get("apiKey"),
  };
  if (kind === "custom") {
    body.baseURL = fd.get("baseURL");
    body.api = fd.get("api") || "openai-completions";
  }
  return body;
}

async function loadEnvs({ silent = false } = {}) {
  // 操作中跳过会替换按钮 DOM 的重绘（g1.4）
  if (envBusy && silent) return;
  if (!envsLoaded && !silent) showSkeleton("envs");
  const data = await api("/api/environments");
  if (envBusy && silent) return;
  envs = data;
  envsLoaded = true;
  renderEnvs();
}

async function loadImages({ silent = false } = {}) {
  if (imageBusy && silent) return;
  if (!imagesLoaded && !silent) showSkeleton("images");
  const data = await api("/api/images");
  if (imageBusy && silent) return;
  images = data;
  imagesLoaded = true;
  renderImages();
  fillVersionSelect();
}

async function loadPresets({ silent = false } = {}) {
  if (presetBusy && silent) return;
  if (!presetsLoaded && !silent) showSkeleton("presets");
  const data = await api("/api/presets");
  if (presetBusy && silent) return;
  presets = data;
  presetsLoaded = true;
  renderPresets();
  fillPresetSelect();
  syncPresetForm();
}

function renderPresets() {
  const tb = $("#preset-rows");
  if (!presets.length) {
    setHTML(tb, `<tr><td colspan="5" class="muted">还没有预设。点右上角新建，密钥只存在本机 data/ 里。</td></tr>`);
    return;
  }
  const html = presets
    .map(
      (p) => `<tr>
        <td class="td-name">${escapeHtml(p.name)}<div class="muted">${escapeHtml(p.id)}</div></td>
        <td data-label="Provider">${escapeHtml(p.provider)}</td>
        <td data-label="Model">${escapeHtml(p.model)}</td>
        <td data-label="密钥">${escapeHtml(p.apiKeyHint || "—")}</td>
        <td class="actions">
          <button class="btn" data-preset-edit="${escapeHtml(p.id)}">编辑</button>
          <button class="btn" data-preset-del="${escapeHtml(p.id)}">删除</button>
        </td>
      </tr>`
    )
    .join("");
  setHTML(tb, html);
}

function showImageError(msg) {
  const el = $("#image-form-error");
  if (!msg) {
    el.hidden = true;
    el.textContent = "";
    return;
  }
  el.hidden = false;
  el.textContent = msg;
}

let remoteReleases = [];

function remoteOptionLabel(r) {
  const bits = [r.version];
  if (r.registered) bits.push("已登记");
  if (r.present) bits.push("本机有");
  else bits.push("本机没有");
  return bits.join(" · ");
}

async function loadRemoteReleases() {
  const sel = $("#github-releases");
  const btn = $("#btn-add-github");
  const hint = $("#github-hint");
  remoteReleases = [];
  btn.disabled = true;
  clearButtonBusy(btn);
  btn.textContent = "添加并拉取";
  hint.hidden = true;
  hint.textContent = "";
  sel.innerHTML = `<option value="">加载中…</option>`;
  try {
    const data = await api("/api/images/remote");
    remoteReleases = data.releases || [];
    if (!remoteReleases.length) {
      sel.innerHTML = `<option value="">暂无公开镜像</option>`;
      return;
    }
    sel.innerHTML = remoteReleases
      .map((r, i) => `<option value="${i}">${escapeHtml(remoteOptionLabel(r))}</option>`)
      .join("");
    const idx = remoteReleases.findIndex((r) => !r.registered);
    sel.value = String(idx >= 0 ? idx : 0);
    btn.disabled = false;
  } catch (err) {
    sel.innerHTML = `<option value="">加载失败</option>`;
    hint.hidden = false;
    hint.textContent = err.message;
  }
}

function openImageModal() {
  showImageError("");
  $("#form-image").reset();
  $("#modal-image").hidden = false;
  loadRemoteReleases();
}

function setPage(next) {
  page = next;
  $$("nav button").forEach((b) => b.classList.toggle("active", b.dataset.nav === next));
  $("#view-envs").hidden = next !== "envs";
  $("#view-presets").hidden = next !== "presets";
  $("#view-images").hidden = next !== "images";
  if (next === "envs") {
    $("#page-title").textContent = "环境";
    $("#page-sub").textContent = "DeepSeek Harness 在线环境";
    $("#btn-primary").textContent = "+ 创建环境";
    if (!envsLoaded) {
      showSkeleton("envs");
      loadEnvs().catch((err) => showBanner(err.message));
    }
  } else if (next === "presets") {
    $("#page-title").textContent = "模型预设";
    $("#page-sub").textContent = "提供方、模型和 API 密钥";
    $("#btn-primary").textContent = "+ 新建预设";
    if (!presetsLoaded) {
      showSkeleton("presets");
      loadPresets().catch((err) => showBanner(err.message));
    }
  } else {
    $("#page-title").textContent = "镜像版本";
    $("#page-sub").textContent = "从公开 GHCR 列表选择并拉取，或手动登记";
    $("#btn-primary").textContent = "+ 登记镜像";
    if (!imagesLoaded) {
      showSkeleton("images");
      loadImages().catch((err) => showBanner(err.message));
    }
  }
}

function openPresetModal(row) {
  const form = $("#form-preset");
  form.reset();
  form.querySelector('[name="id"]').value = row ? row.id : "";
  $("#preset-title").textContent = row ? "编辑预设" : "新建预设";
  const key = form.querySelector('[name="apiKey"]');
  key.required = !row;
  key.placeholder = row ? "留空则不修改密钥" : "输入 API 密钥";
  loadProviders()
    .then(() => {
      if (!row) {
        syncProviderFields(form);
        $("#modal-preset").hidden = false;
        return;
      }
      form.querySelector('[name="name"]').value = row.name;
      const providerSel = form.querySelector('[name="provider"]');
      const known = [...providerSel.options].some((o) => o.value === row.provider);
      if (known) {
        providerSel.value = row.provider;
      } else {
        providerSel.value = "custom";
        form.querySelector('[name="providerId"]').value = row.provider;
        form.querySelector('[name="baseURL"]').value = row.baseURL || "";
        form.querySelector('[name="api"]').value = row.api || "";
      }
      syncProviderFields(form);
      const modelSel = form.querySelector('[name="model"]');
      if (modelSel && [...modelSel.options].some((o) => o.value === row.model)) {
        modelSel.value = row.model;
      } else {
        form.querySelector('[name="modelId"]').value = row.model;
      }
      $("#modal-preset").hidden = false;
    })
    .catch((err) => showBanner(err.message));
}

// 打开「创建环境」弹窗时,自动填充上次提交的预装插件(每行一个);无记录保持空白
function fillLastPlugins() {
  const ta = document.querySelector('#form-create [name="plugins"]');
  const hint = $("#plugins-hint");
  if (ta) ta.value = "";
  if (hint) hint.hidden = true;
  const list = readLastPlugins();
  if (!ta || !list || !list.length) return;
  ta.value = list.join("\n");
  if (hint) hint.hidden = false;
}

$("#btn-primary").addEventListener("click", () => {
  showBanner("");
  if (page === "envs") {
    fillVersionSelect();
    loadProviders().then(() => {
      fillPresetSelect();
      syncPresetForm();
    }).catch((err) => showBanner(err.message));
    fillLastPlugins();
    $("#modal-create").hidden = false;
  } else if (page === "presets") {
    openPresetModal(null);
  } else {
    openImageModal();
  }
});

$$("nav button").forEach((b) =>
  b.addEventListener("click", () => {
    setPage(b.dataset.nav);
    showBanner("");
    setDrawer(false);
  })
);

// ===== 移动端汉堡抽屉 =====
const drawer = $("#drawer");
const drawerMask = $("#drawer-mask");
const drawerToggle = $("#drawer-toggle");

function setDrawer(open) {
  drawer.classList.toggle("open", open);
  drawerMask.hidden = !open;
  drawerToggle.setAttribute("aria-expanded", String(open));
}

drawerToggle.addEventListener("click", () => {
  setDrawer(!drawer.classList.contains("open"));
});
drawerMask.addEventListener("click", () => setDrawer(false));

// 跨断点缩放视口时收起抽屉,避免遮罩/滑出态残留在桌面布局
const mobileMQ = window.matchMedia("(max-width: 768px)");
if (mobileMQ.addEventListener) {
  mobileMQ.addEventListener("change", (ev) => {
    if (!ev.matches) setDrawer(false);
  });
} else if (mobileMQ.addListener) {
  mobileMQ.addListener((ev) => {
    if (!ev.matches) setDrawer(false);
  });
}

$("#btn-create-cancel").addEventListener("click", () => {
  $("#modal-create").hidden = true;
});
$("#btn-preset-cancel").addEventListener("click", () => {
  $("#modal-preset").hidden = true;
});
$("#btn-image-cancel").addEventListener("click", () => {
  $("#modal-image").hidden = true;
});
$("#btn-add-github").addEventListener("click", async () => {
  const idx = Number($("#github-releases").value);
  const r = remoteReleases[idx];
  if (!r) return;
  const btn = $("#btn-add-github");
  const cancel = $("#btn-image-cancel");
  const dialog = $("#modal-image .dialog");
  showImageError("");
  setSubmitBusy(btn, cancel, "正在拉取…");
  showOverlay(dialog, "正在拉取…");
  imageBusy = true;
  try {
    await api("/api/images", {
      method: "POST",
      body: JSON.stringify({ version: r.version, ref: r.ref, pull: true }),
    });
    $("#modal-image").hidden = true;
    await loadImages();
  } catch (err) {
    showImageError(err.message);
  } finally {
    imageBusy = false;
    hideOverlay(dialog);
    clearSubmitBusy(btn, cancel, "添加并拉取");
  }
});
$("#btn-logs-close").addEventListener("click", () => {
  $("#modal-logs").hidden = true;
});

document.querySelector('#form-create [name="modelPreset"]').addEventListener("change", syncPresetForm);
document.querySelector('#form-create [name="provider"]').addEventListener("change", () => syncProviderFields($("#form-create")));
document.querySelector('#form-preset [name="provider"]').addEventListener("change", () => syncProviderFields($("#form-preset")));

$("#form-create").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const preset = selectedPreset();
  const body = {
    name: fd.get("name"),
    dshVersion: fd.get("dshVersion"),
    plugins: String(fd.get("plugins") || "")
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean),
  };
  if (preset) {
    body.presetId = preset.id;
  } else {
    Object.assign(body, presetBody(ev.target));
  }
  const submit = ev.target.querySelector('button[type="submit"]');
  const cancel = $("#btn-create-cancel");
  const dialog = $("#modal-create .dialog");
  setSubmitBusy(submit, cancel, "创建中…");
  showOverlay(dialog, "创建中…");
  envBusy = true;
  try {
    await api("/api/environments", { method: "POST", body: JSON.stringify(body) });
    saveLastPlugins(body.plugins);
    $("#modal-create").hidden = true;
    ev.target.reset();
    fillPresetSelect();
    syncPresetForm();
    await loadEnvs();
  } catch (err) {
    showBanner(err.message);
  } finally {
    envBusy = false;
    hideOverlay(dialog);
    clearSubmitBusy(submit, cancel, "创建");
  }
});

$("#form-preset").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const id = ev.target.querySelector('[name="id"]').value;
  const body = presetBody(ev.target);
  const submit = ev.target.querySelector('button[type="submit"]');
  const cancel = $("#btn-preset-cancel");
  const dialog = $("#modal-preset .dialog");
  setSubmitBusy(submit, cancel, "保存中…");
  showOverlay(dialog, "保存中…");
  presetBusy = true;
  try {
    if (id) {
      await api(`/api/presets/${id}`, { method: "PUT", body: JSON.stringify(body) });
    } else {
      await api("/api/presets", { method: "POST", body: JSON.stringify(body) });
    }
    $("#modal-preset").hidden = true;
    ev.target.reset();
    await loadPresets();
  } catch (err) {
    showBanner(err.message);
  } finally {
    presetBusy = false;
    hideOverlay(dialog);
    clearSubmitBusy(submit, cancel, "保存");
  }
});

$("#form-image").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const body = { version: fd.get("version"), ref: fd.get("ref"), pull: true };
  const submit = ev.target.querySelector('button[type="submit"]');
  const cancel = $("#btn-image-cancel");
  const dialog = $("#modal-image .dialog");
  try {
    showImageError("");
    setSubmitBusy(submit, cancel, "正在拉取…");
    showOverlay(dialog, "正在拉取…");
    imageBusy = true;
    await api("/api/images", { method: "POST", body: JSON.stringify(body) });
    $("#modal-image").hidden = true;
    ev.target.reset();
    await loadImages();
  } catch (err) {
    showImageError(err.message);
  } finally {
    imageBusy = false;
    hideOverlay(dialog);
    clearSubmitBusy(submit, cancel, "保存并拉取");
  }
});

$("#env-rows").addEventListener("click", async (ev) => {
  const btn = ev.target.closest("button[data-act]");
  if (!btn || btn.disabled) return;
  const id = btn.dataset.id;
  const act = btn.dataset.act;
  const row = envs.find((e) => e.id === id);
  if (act === "open") {
    if (row && row.openURL) window.open(row.openURL, "_blank");
    return;
  }
  // 销毁的 confirm 取消分支不进请求,按钮保持可用
  if (act === "destroy" && !confirm("销毁后容器和该环境的 DSH_HOME 都会删掉。")) return;

  const labels = ACT_LABELS[act] || { idle: btn.textContent.trim(), busy: "处理中…" };
  // g1.1: 点击当下同步转圈+文案（不等网络）
  setButtonBusy(btn, labels.busy);
  const wrap = $("#env-table-wrap");
  showOverlay(wrap, labels.busy);
  envBusy = true;
  try {
    if (act === "logs") {
      $("#logs-title").textContent = `日志 · ${row ? row.name : id}`;
      $("#logs-body").textContent = "加载中…";
      $("#modal-logs").hidden = false;
      const data = await api(`/api/environments/${id}/logs`);
      $("#logs-body").textContent = data.logs || "(empty)";
      return;
    }
    if (act === "renew") {
      // g1.4: 用 POST 返回体立刻更新 TTL，不等下次全量列表
      const renewed = await api(`/api/environments/${id}/renew`, { method: "POST" });
      if (renewed && renewed.destroyAt) {
        const idx = envs.findIndex((e) => e.id === id);
        if (idx >= 0) envs[idx] = { ...envs[idx], ...renewed };
      }
      return;
    }
    if (act === "stop") await api(`/api/environments/${id}/stop`, { method: "POST" });
    if (act === "restart") await api(`/api/environments/${id}/restart`, { method: "POST" });
    if (act === "start") await api(`/api/environments/${id}/start`, { method: "POST" });
    if (act === "destroy") await api(`/api/environments/${id}`, { method: "DELETE" });
    await loadEnvs();
  } catch (err) {
    showBanner(err.message);
    if (act === "logs") $("#modal-logs").hidden = true;
  } finally {
    envBusy = false;
    hideOverlay(wrap);
    // 成功续期：只刷新 TTL 文案并恢复按钮，避免整表冲掉；其他操作列表可能已重渲
    if (act === "renew" && row) {
      const tr = $(`#env-rows tr[data-env-id="${CSS.escape(id)}"]`);
      if (tr) {
        const ttlCell = tr.querySelector('[data-label="TTL"]');
        const updated = envs.find((e) => e.id === id);
        if (ttlCell && updated) ttlCell.innerHTML = formatTTL(updated.destroyAt);
        tr.querySelectorAll("button[data-act]").forEach((b) => {
          if (b.dataset.act === "open") return;
          clearButtonBusy(b);
          const l = ACT_LABELS[b.dataset.act];
          if (l) b.textContent = l.idle;
        });
      } else {
        await loadEnvs();
      }
    } else if (act === "logs") {
      clearButtonBusy(btn);
      const rowEl = btn.closest("tr");
      if (rowEl) {
        rowEl.querySelectorAll("button[data-act]").forEach((b) => {
          if (b.dataset.act !== "open") b.disabled = false;
        });
      }
    }
    // stop/start/restart/destroy: loadEnvs 已换新 DOM
  }
});

$("#image-rows").addEventListener("click", async (ev) => {
  const pullBtn = ev.target.closest("button[data-img-pull]");
  if (pullBtn) {
    if (pullBtn.disabled) return;
    const version = pullBtn.dataset.imgPull;
    const row = images.find((im) => im.version === version);
    if (!row) return;
    setButtonBusy(pullBtn, "正在拉取…");
    showOverlay($("#image-table-wrap"), "正在拉取…");
    imageBusy = true;
    try {
      await api("/api/images", {
        method: "POST",
        body: JSON.stringify({ version: row.version, ref: row.ref, pull: true }),
      });
      await loadImages();
    } catch (err) {
      showBanner(err.message);
      clearButtonBusy(pullBtn);
      pullBtn.textContent = "拉取";
    } finally {
      imageBusy = false;
      hideOverlay($("#image-table-wrap"));
    }
    return;
  }
  const btn = ev.target.closest("button[data-img-del]");
  if (!btn || btn.disabled) return;
  const version = btn.dataset.imgDel;
  if (!confirm(`从目录里删除 ${version}？不会删除 Docker 里的镜像。`)) return;
  setButtonBusy(btn, "删除中…");
  showOverlay($("#image-table-wrap"), "删除中…");
  imageBusy = true;
  try {
    await api(`/api/images/${encodeURIComponent(version)}`, { method: "DELETE" });
    await loadImages();
  } catch (err) {
    showBanner(err.message);
    clearButtonBusy(btn);
  } finally {
    imageBusy = false;
    hideOverlay($("#image-table-wrap"));
  }
});

$("#preset-rows").addEventListener("click", async (ev) => {
  const edit = ev.target.closest("button[data-preset-edit]");
  if (edit) {
    const row = presets.find((p) => p.id === edit.dataset.presetEdit);
    if (row) openPresetModal(row);
    return;
  }
  const del = ev.target.closest("button[data-preset-del]");
  if (!del || del.disabled) return;
  const id = del.dataset.presetDel;
  const row = presets.find((p) => p.id === id);
  if (!confirm(`删除预设 ${row ? row.name : id}？不会影响已创建的环境。`)) return;
  setButtonBusy(del, "删除中…");
  showOverlay($("#preset-table-wrap"), "删除中…");
  presetBusy = true;
  try {
    await api(`/api/presets/${id}`, { method: "DELETE" });
    await loadPresets();
  } catch (err) {
    showBanner(err.message);
    clearButtonBusy(del);
  } finally {
    presetBusy = false;
    hideOverlay($("#preset-table-wrap"));
  }
});

async function tick() {
  try {
    // 背景轮询：silent，不打骨架/遮罩；envBusy 时跳过环境表重绘
    if (page === "envs") await loadEnvs({ silent: true });
    if (!imageBusy) await loadImages({ silent: true });
    if (!presetBusy) await loadPresets({ silent: true });
  } catch (err) {
    showBanner(err.message);
  }
}

showSkeleton("envs");
tick();
setInterval(tick, 2000);
