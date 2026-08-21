const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => [...document.querySelectorAll(sel)];

let page = "envs";
let images = [];
let envs = [];
let presets = [];
let imageBusy = false;

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
      return `<tr>
        <td>${escapeHtml(e.name)}<div class="muted">${escapeHtml(e.id)}</div></td>
        <td>${statusPill(e.status)}${e.error ? `<div class="muted">${escapeHtml(e.error)}</div>` : ""}</td>
        <td>${healthPill(e.health)}</td>
        <td>${formatTTL(e.destroyAt)}</td>
        <td>${escapeHtml(e.dshVersion)}</td>
        <td>${escapeHtml(e.provider)}</td>
        <td>${escapeHtml(e.model)}</td>
        <td>${pluginsCell(e.plugins)}</td>
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
        <td>${escapeHtml(im.version)}</td>
        <td>${escapeHtml(im.ref)}</td>
        <td>${im.present ? statusPill("ready") : statusPill("missing")}${im.error ? `<div class="muted">${escapeHtml(im.error)}</div>` : ""}</td>
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

async function loadEnvs() {
  envs = await api("/api/environments");
  renderEnvs();
}

async function loadImages() {
  images = await api("/api/images");
  renderImages();
  fillVersionSelect();
}

async function loadPresets() {
  presets = await api("/api/presets");
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
        <td>${escapeHtml(p.name)}<div class="muted">${escapeHtml(p.id)}</div></td>
        <td>${escapeHtml(p.provider)}</td>
        <td>${escapeHtml(p.model)}</td>
        <td>${escapeHtml(p.apiKeyHint || "—")}</td>
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
  } else if (next === "presets") {
    $("#page-title").textContent = "模型预设";
    $("#page-sub").textContent = "提供方、模型和 API 密钥";
    $("#btn-primary").textContent = "+ 新建预设";
  } else {
    $("#page-title").textContent = "镜像版本";
    $("#page-sub").textContent = "从公开 GHCR 列表选择并拉取，或手动登记";
    $("#btn-primary").textContent = "+ 登记镜像";
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

$("#btn-primary").addEventListener("click", () => {
  showBanner("");
  if (page === "envs") {
    fillVersionSelect();
    loadProviders().then(() => {
      fillPresetSelect();
      syncPresetForm();
    }).catch((err) => showBanner(err.message));
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
  })
);

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
  showImageError("");
  btn.disabled = true;
  const prev = btn.textContent;
  btn.textContent = "正在拉取…";
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
    btn.disabled = false;
    btn.textContent = prev || "添加并拉取";
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
  try {
    await api("/api/environments", { method: "POST", body: JSON.stringify(body) });
    $("#modal-create").hidden = true;
    ev.target.reset();
    fillPresetSelect();
    syncPresetForm();
    await loadEnvs();
  } catch (err) {
    showBanner(err.message);
  }
});

$("#form-preset").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const id = ev.target.querySelector('[name="id"]').value;
  const body = presetBody(ev.target);
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
  }
});

$("#form-image").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const body = { version: fd.get("version"), ref: fd.get("ref"), pull: true };
  const submit = ev.target.querySelector('button[type="submit"]');
  try {
    showImageError("");
    submit.disabled = true;
    submit.textContent = "正在拉取…";
    imageBusy = true;
    await api("/api/images", { method: "POST", body: JSON.stringify(body) });
    $("#modal-image").hidden = true;
    ev.target.reset();
    await loadImages();
  } catch (err) {
    showImageError(err.message);
  } finally {
    imageBusy = false;
    submit.disabled = false;
    submit.textContent = "保存并拉取";
  }
});

$("#env-rows").addEventListener("click", async (ev) => {
  const btn = ev.target.closest("button[data-act]");
  if (!btn) return;
  const id = btn.dataset.id;
  const row = envs.find((e) => e.id === id);
  try {
    if (btn.dataset.act === "open") {
      if (row && row.openURL) window.open(row.openURL, "_blank");
      return;
    }
    if (btn.dataset.act === "logs") {
      const data = await api(`/api/environments/${id}/logs`);
      $("#logs-title").textContent = `日志 · ${row ? row.name : id}`;
      $("#logs-body").textContent = data.logs || "(empty)";
      $("#modal-logs").hidden = false;
      return;
    }
    if (btn.dataset.act === "renew") await api(`/api/environments/${id}/renew`, { method: "POST" });
    if (btn.dataset.act === "stop") await api(`/api/environments/${id}/stop`, { method: "POST" });
    if (btn.dataset.act === "restart") await api(`/api/environments/${id}/restart`, { method: "POST" });
    if (btn.dataset.act === "start") await api(`/api/environments/${id}/start`, { method: "POST" });
    if (btn.dataset.act === "destroy") {
      if (!confirm("销毁后容器和该环境的 DSH_HOME 都会删掉。")) return;
      await api(`/api/environments/${id}`, { method: "DELETE" });
    }
    await loadEnvs();
  } catch (err) {
    showBanner(err.message);
  }
});

$("#image-rows").addEventListener("click", async (ev) => {
  const pullBtn = ev.target.closest("button[data-img-pull]");
  if (pullBtn) {
    const version = pullBtn.dataset.imgPull;
    const row = images.find((im) => im.version === version);
    if (!row) return;
    pullBtn.disabled = true;
    pullBtn.textContent = "拉取中…";
    imageBusy = true;
    try {
      await api("/api/images", {
        method: "POST",
        body: JSON.stringify({ version: row.version, ref: row.ref, pull: true }),
      });
      await loadImages();
    } catch (err) {
      showBanner(err.message);
      pullBtn.disabled = false;
      pullBtn.textContent = "拉取";
    } finally {
      imageBusy = false;
    }
    return;
  }
  const btn = ev.target.closest("button[data-img-del]");
  if (!btn) return;
  const version = btn.dataset.imgDel;
  if (!confirm(`从目录里删除 ${version}？不会删除 Docker 里的镜像。`)) return;
  try {
    await api(`/api/images/${encodeURIComponent(version)}`, { method: "DELETE" });
    await loadImages();
  } catch (err) {
    showBanner(err.message);
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
  if (!del) return;
  const id = del.dataset.presetDel;
  const row = presets.find((p) => p.id === id);
  if (!confirm(`删除预设 ${row ? row.name : id}？不会影响已创建的环境。`)) return;
  try {
    await api(`/api/presets/${id}`, { method: "DELETE" });
    await loadPresets();
  } catch (err) {
    showBanner(err.message);
  }
});

async function tick() {
  try {
    if (page === "envs") await loadEnvs();
    if (!imageBusy) await loadImages();
    await loadPresets();
  } catch (err) {
    showBanner(err.message);
  }
}

tick();
setInterval(tick, 2000);
