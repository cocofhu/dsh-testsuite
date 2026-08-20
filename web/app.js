const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => [...document.querySelectorAll(sel)];

let page = "envs";
let images = [];
let envs = [];

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

function setHTML(el, html) {
  if (el.dataset.html === html) return false;
  el.dataset.html = html;
  el.innerHTML = html;
  return true;
}

function renderEnvs() {
  const tb = $("#env-rows");
  if (!envs.length) {
    setHTML(tb, `<tr><td colspan="8" class="muted">还没有环境。先到「镜像版本」登记已构建的 runtime 镜像，再创建环境。</td></tr>`);
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
        <td>${escapeHtml(e.dshVersion)}</td>
        <td>${escapeHtml(e.provider)}</td>
        <td>${escapeHtml(e.model)}</td>
        <td>${pluginsCell(e.plugins)}</td>
        <td class="actions">
          <button class="btn" data-act="open" data-id="${e.id}" ${healthy && e.openURL ? "" : "disabled"} ${running && !healthy ? 'title="等待 Health"' : ""}>打开</button>
          <button class="btn" data-act="logs" data-id="${e.id}">日志</button>
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
  const sel = document.querySelector('#form-create [name="provider"]');
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
  syncProviderForm();
}

function providerKind() {
  const sel = document.querySelector('#form-create [name="provider"]');
  const opt = sel.selectedOptions[0];
  return (opt && opt.dataset.kind) || "official";
}

function selectedProvider() {
  const id = document.querySelector('#form-create [name="provider"]').value;
  return providerOptions.find((p) => p.id === id);
}

function modelLabel(m) {
  if (m.name && m.name !== m.id) return `${m.name} (${m.id})`;
  return m.id;
}

function fillModels() {
  const kind = providerKind();
  const models = (selectedProvider() && selectedProvider().models) || [];
  const modelSel = document.querySelector('#form-create [name="model"]');
  const selectWrap = $("#model-select-wrap");
  const customWrap = $("#custom-model");
  const modelId = document.querySelector('#form-create [name="modelId"]');
  const useSelect = kind !== "custom" && models.length > 0;
  selectWrap.hidden = !useSelect;
  modelSel.disabled = !useSelect;
  modelSel.required = useSelect;
  customWrap.hidden = useSelect;
  modelId.required = !useSelect;
  modelId.disabled = useSelect;
  if (useSelect) {
    modelSel.innerHTML = models
      .map((m) => `<option value="${escapeHtml(m.id)}">${escapeHtml(modelLabel(m))}</option>`)
      .join("");
  }
}

function syncProviderForm() {
  const custom = providerKind() === "custom";
  $("#custom-provider-id").hidden = !custom;
  $("#custom-base-url").hidden = !custom;
  $("#custom-api").hidden = !custom;
  const providerId = document.querySelector('#form-create [name="providerId"]');
  const base = document.querySelector('#form-create [name="baseURL"]');
  providerId.required = custom;
  base.required = custom;
  fillModels();
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
  remoteReleases = [];
  btn.disabled = true;
  btn.textContent = "添加选中版本";
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
    $("#github-hint").textContent = err.message;
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
  $("#view-images").hidden = next !== "images";
  if (next === "envs") {
    $("#page-title").textContent = "环境";
    $("#page-sub").textContent = "DeepSeek Harness 在线环境";
    $("#btn-primary").textContent = "+ 创建环境";
  } else {
    $("#page-title").textContent = "镜像版本";
    $("#page-sub").textContent = "从公开 GHCR 列表选择，或手动登记";
    $("#btn-primary").textContent = "+ 登记镜像";
  }
}

$("#btn-primary").addEventListener("click", () => {
  showBanner("");
  if (page === "envs") {
    fillVersionSelect();
    loadProviders().catch((err) => showBanner(err.message));
    $("#modal-create").hidden = false;
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
  try {
    await api("/api/images", {
      method: "POST",
      body: JSON.stringify({ version: r.version, ref: r.ref }),
    });
    $("#modal-image").hidden = true;
    await loadImages();
  } catch (err) {
    showImageError(err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = "添加选中版本";
  }
});
$("#btn-logs-close").addEventListener("click", () => {
  $("#modal-logs").hidden = true;
});

document.querySelector('#form-create [name="provider"]').addEventListener("change", syncProviderForm);

$("#form-create").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const kind = providerKind();
  const body = {
    name: fd.get("name"),
    dshVersion: fd.get("dshVersion"),
    apiKey: fd.get("apiKey"),
    provider: kind === "custom" ? fd.get("providerId") : fd.get("provider"),
    model: kind === "custom" || !fd.get("model") ? fd.get("modelId") : fd.get("model"),
    plugins: String(fd.get("plugins") || "")
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean),
  };
  if (kind === "custom") {
    body.baseURL = fd.get("baseURL");
    body.api = fd.get("api") || "openai-completions";
  }
  try {
    await api("/api/environments", { method: "POST", body: JSON.stringify(body) });
    $("#modal-create").hidden = true;
    ev.target.reset();
    syncProviderForm();
    await loadEnvs();
  } catch (err) {
    showBanner(err.message);
  }
});

$("#form-image").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const fd = new FormData(ev.target);
  const body = { version: fd.get("version"), ref: fd.get("ref") };
  try {
    showImageError("");
    await api("/api/images", { method: "POST", body: JSON.stringify(body) });
    $("#modal-image").hidden = true;
    ev.target.reset();
    await loadImages();
  } catch (err) {
    showImageError(err.message);
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

async function tick() {
  try {
    if (page === "envs") await loadEnvs();
    await loadImages();
  } catch (err) {
    showBanner(err.message);
  }
}

tick();
setInterval(tick, 2000);
