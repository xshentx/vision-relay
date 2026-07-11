const form = document.querySelector("#configForm");
const statusEl = document.querySelector("#status");
const toast = document.querySelector("#toast");
const serviceState = document.querySelector("#serviceState");
const imageInput = document.querySelector("#image");
const preview = document.querySelector("#preview");
const output = document.querySelector("#output");
const visionOutput = document.querySelector("#visionOutput");
const send = document.querySelector("#send");
const reloadConfig = document.querySelector("#reloadConfig");
const refreshLogs = document.querySelector("#refreshLogs");
const clearLogs = document.querySelector("#clearLogs");
const logList = document.querySelector("#logList");
const logPageSize = document.querySelector("#logPageSize");
const logPageInfo = document.querySelector("#logPageInfo");
const prevLogPage = document.querySelector("#prevLogPage");
const nextLogPage = document.querySelector("#nextLogPage");
const fileName = document.querySelector("#fileName");
const segments = [...document.querySelectorAll(".segment")];
const generateKey = document.querySelector("#generateKey");
const clientKeyName = document.querySelector("#clientKeyName");
const clientKeyList = document.querySelector("#clientKeyList");
const opencodeConfig = document.querySelector("#opencodeConfig");
const configureOpenCode = document.querySelector("#configureOpenCode");
const codexConfig = document.querySelector("#codexConfig");
const configureCodex = document.querySelector("#configureCodex");

const claudeCodeConfig = document.querySelector("#claudeCodeConfig");
const configureClaudeCode = document.querySelector("#configureClaudeCode");
const textProfileList = document.querySelector("#textProfileList");
const addTextProfile = document.querySelector("#addTextProfile");
const visionProfileList = document.querySelector("#visionProfileList");
const visionEnabledInput = document.querySelector("#visionEnabled");
const addVisionProfile = document.querySelector("#addVisionProfile");
const profileModal = document.querySelector("#profileModal");
const profileModalForm = document.querySelector("#profileModalForm");
const profileModalTitle = document.querySelector("#profileModalTitle");
const profileModalHelp = document.querySelector("#profileModalHelp");
const profileModalSubmit = document.querySelector("#profileModalSubmit");
const closeProfileModal = document.querySelector("#closeProfileModal");
const cancelProfileModal = document.querySelector("#cancelProfileModal");
const modalProfileName = document.querySelector("#modalProfileName");
const modalProfileProvider = document.querySelector("#modalProfileProvider");
const modalProfileWireAPIWrap = document.querySelector("#modalProfileWireAPIWrap");
const modalProfileWireAPI = document.querySelector("#modalProfileWireAPI");
const modalProfileBaseURL = document.querySelector("#modalProfileBaseURL");
const modalProfileAPIKey = document.querySelector("#modalProfileAPIKey");
const modalProfileModelWrap = document.querySelector("#modalProfileModelWrap");
const modalProfileModelLabel = document.querySelector("#modalProfileModelLabel");
const modalProfileModel = document.querySelector("#modalProfileModel");
const fetchModels = document.querySelector("#fetchModels");
const fetchModelsForMapping = document.querySelector("#fetchModelsForMapping");
const addModelMapping = document.querySelector("#addModelMapping");
const modelMappingSection = document.querySelector("#modelMappingSection");
const modelMappingRows = document.querySelector("#modelMappingRows");
const modelPickerPanel = document.querySelector("#modelPickerPanel");
const modelSearch = document.querySelector("#modelSearch");
const modelSelect = document.querySelector("#modelSelect");
const addFetchedModels = document.querySelector("#addFetchedModels");
const modelPickerStatus = document.querySelector("#modelPickerStatus");
const modalProfileProxyWrap = document.querySelector("#modalProfileProxyWrap");
const modalProfileProxyURL = document.querySelector("#modalProfileProxyURL");
const modalProfileSupportsImagesWrap = document.querySelector("#modalProfileSupportsImagesWrap");
const modalProfileSupportsImages = document.querySelector("#modalProfileSupportsImages");
const navItems = [...document.querySelectorAll(".nav-item")];
const pages = [...document.querySelectorAll("[data-page-panel]")];
const homeJumpButtons = [...document.querySelectorAll(".home-jump")];
const homeBaseURL = document.querySelector("#homeBaseURL");
const homeTextModel = document.querySelector("#homeTextModel");
const homeTextProvider = document.querySelector("#homeTextProvider");
const homeVisionModel = document.querySelector("#homeVisionModel");
const homeVisionProvider = document.querySelector("#homeVisionProvider");
const homeKeyCount = document.querySelector("#homeKeyCount");
const homeTextProfile = document.querySelector("#homeTextProfile");
const homeVisionProfile = document.querySelector("#homeVisionProfile");
const homeProxyState = document.querySelector("#homeProxyState");

let imageDataUrl = "";
let testMode = "chat";
let clientKeys = [];
let textProfiles = [];
let activeTextProfileId = "";
let visionProfiles = [];
let activeVisionProfileId = "";
let visionCapabilityEnabled = true;
let toastTimer = 0;
let currentConfig = {};
let profileModalKind = "text";
let profileModalMode = "create";
let profileModalEditId = "";
let fetchedModels = [];
let modalModelMappings = [];
let currentLogPage = 1;
let currentLogTotal = 0;

const endpoints = {
  openaiBaseEndpoint: `${location.origin}/v1`,
  responsesEndpoint: `${location.origin}/v1/responses`,
  chatEndpoint: `${location.origin}/v1/chat/completions`,
  anthropicBaseEndpoint: location.origin,
  anthropicMessagesEndpoint: `${location.origin}/v1/messages`,
  geminiBaseEndpoint: location.origin,
  geminiGenerateEndpoint: `${location.origin}/v1beta/models/{model}:generateContent`,
  ollamaChatEndpoint: `${location.origin}/api/chat`,
  ollamaGenerateEndpoint: `${location.origin}/api/generate`
};

for (const [id, value] of Object.entries(endpoints)) {
  const el = document.querySelector(`#${id}`);
  if (el) el.textContent = value;
}

navItems.forEach((item) => {
  item.addEventListener("click", () => {
    showPage(item.dataset.page);
    if (item.dataset.page === "logs") {
      loadLogs().catch((err) => {
        console.error(err);
        showToast(`加载日志失败：${err.message || err}`, "error");
      });
    }
  });
});

homeJumpButtons.forEach((button) => {
  button.addEventListener("click", () => {
    showPage(button.dataset.targetPage);
  });
});

function showPage(page) {
  navItems.forEach((item) => item.classList.toggle("active", item.dataset.page === page));
  pages.forEach((panel) => panel.classList.toggle("active", panel.dataset.pagePanel === page));
}

function setStatus(text) {
  statusEl.textContent = text;
}

function showToast(message, type = "info") {
  clearTimeout(toastTimer);
  toast.textContent = message;
  toast.className = `toast show ${type}`;
  toastTimer = setTimeout(() => {
    toast.className = "toast";
  }, 3200);
}

async function readErrorMessage(res) {
  try {
    const payload = await res.json();
    return payload?.error?.message || `HTTP ${res.status}`;
  } catch {
    return await res.text() || `HTTP ${res.status}`;
  }
}

async function loadConfig() {
  const res = await fetch("/api/config");
  if (!res.ok) throw new Error(`config ${res.status}`);
  const cfg = await res.json();
  currentConfig = cfg;
  visionCapabilityEnabled = cfg.vision_enabled !== false;
  if (visionEnabledInput) {
    visionEnabledInput.checked = visionCapabilityEnabled;
  }
  const migrated = migrateProfiles(cfg);
  textProfiles = normalizeTextProfiles(cfg.text_model_profiles || migrated.textProfiles);
  activeTextProfileId = cfg.active_text_profile_id || migrated.activeTextProfileId || textProfiles[0].id;
  if (!textProfiles.some((profile) => profile.id === activeTextProfileId)) {
    activeTextProfileId = textProfiles[0].id;
  }
  visionProfiles = normalizeVisionProfiles(cfg.vision_model_profiles || migrated.visionProfiles);
  activeVisionProfileId = cfg.active_vision_profile_id || migrated.activeVisionProfileId || visionProfiles[0].id;
  if (!visionProfiles.some((profile) => profile.id === activeVisionProfileId)) {
    activeVisionProfileId = visionProfiles[0].id;
  }
  for (const [key, value] of Object.entries(cfg)) {
    const field = form.elements[key];
    if (!field) continue;
    if (field.type === "checkbox") {
      field.checked = Boolean(value);
    } else {
      field.value = value ?? "";
    }
  }
  renderTextProfiles();
  applyTextProfile(activeTextProfileId);
  renderVisionProfiles();
  applyVisionProfile(activeVisionProfileId);
  clientKeys = normalizeClientKeys(cfg.client_api_key_entries || keysToEntries(cfg.client_api_keys || []));
  renderClientKeys();
  renderOverview();
  setStatus("已加载");
  serviceState.textContent = "在线";
}

reloadConfig.addEventListener("click", () => {
  loadConfig().catch((err) => {
    console.error(err);
    setStatus("加载失败");
    serviceState.textContent = "离线";
  });
});

refreshLogs.addEventListener("click", () => {
  loadLogs(currentLogPage).catch((err) => {
    console.error(err);
    showToast(`加载日志失败：${err.message || err}`, "error");
  });
});

clearLogs.addEventListener("click", async () => {
  clearLogs.disabled = true;
  try {
    const res = await fetch("/api/logs", {method: "DELETE"});
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    currentLogPage = 1;
    await loadLogs(currentLogPage);
    showToast("日志已清空", "success");
  } catch (err) {
    console.error(err);
    showToast(`清空日志失败：${err.message || err}`, "error");
  } finally {
    clearLogs.disabled = false;
  }
});

logPageSize.addEventListener("change", () => {
  currentLogPage = 1;
  loadLogs(currentLogPage).catch((err) => {
    console.error(err);
    showToast(`加载日志失败：${err.message || err}`, "error");
  });
});

prevLogPage.addEventListener("click", () => {
  if (currentLogPage <= 1) return;
  loadLogs(currentLogPage - 1).catch((err) => {
    console.error(err);
    showToast(`加载日志失败：${err.message || err}`, "error");
  });
});

nextLogPage.addEventListener("click", () => {
  const totalPages = Math.max(1, Math.ceil(currentLogTotal / Number(logPageSize.value || 20)));
  if (currentLogPage >= totalPages) return;
  loadLogs(currentLogPage + 1).catch((err) => {
    console.error(err);
    showToast(`加载日志失败：${err.message || err}`, "error");
  });
});

addTextProfile.addEventListener("click", () => {
  openProfileModal("text", "create");
});

addVisionProfile.addEventListener("click", () => {
  openProfileModal("vision", "create");
});

visionEnabledInput?.addEventListener("change", () => {
  visionCapabilityEnabled = visionEnabledInput.checked;
  renderVisionProfiles();
  renderOpenCodeSnippet();
  renderOverview();
  const message = visionCapabilityEnabled ? "已开启视觉模型能力" : "已关闭视觉模型能力";
  persistConfig(message).catch((err) => {
    console.error(err);
    showToast(`保存失败：${err.message || err}`, "error");
  });
});

closeProfileModal.addEventListener("click", () => {
  closeModal();
});

cancelProfileModal.addEventListener("click", () => {
  closeModal();
});

modalProfileProvider.addEventListener("change", () => {
  const current = modalProfileBaseURL.value.trim();
  const defaults = ["https://api.openai.com", "https://api.anthropic.com", "https://generativelanguage.googleapis.com", "http://127.0.0.1:11434"];
  if (!current || defaults.includes(current)) {
    modalProfileBaseURL.value = defaultBaseURL(modalProfileProvider.value);
  }
  resetModelPicker();
});

modalProfileBaseURL.addEventListener("input", resetModelPicker);
modalProfileAPIKey.addEventListener("input", resetModelPicker);

fetchModels.addEventListener("click", () => {
  fetchProviderModels().catch((err) => {
    console.error(err);
    modelPickerPanel.hidden = false;
    modelPickerStatus.textContent = `获取失败：${err.message || err}`;
    showToast(`获取模型失败：${err.message || err}`, "error");
  });
});

fetchModelsForMapping.addEventListener("click", () => {
  fetchProviderModels().catch((err) => {
    console.error(err);
    modelPickerPanel.hidden = false;
    modelPickerStatus.textContent = `获取失败：${err.message || err}`;
    showToast(`获取模型失败：${err.message || err}`, "error");
  });
});

addModelMapping.addEventListener("click", () => {
  addModelMappingRow();
});

modelSearch.addEventListener("input", () => {
  renderFetchedModels();
});

modelSelect.addEventListener("change", () => {
  updateSelectedModelStatus();
});

modelSelect.addEventListener("dblclick", () => {
  addSelectedModelsToModal(true);
});

addFetchedModels.addEventListener("click", () => {
  addSelectedModelsToModal(true);
});

profileModal.addEventListener("click", (event) => {
  if (event.target === profileModal) {
    closeModal();
  }
});

profileModalForm.addEventListener("submit", (event) => {
  event.preventDefault();
  createProfileFromModal().catch((err) => {
    console.error(err);
    showToast(`保存失败：${err.message || err}`, "error");
  });
});

async function persistConfig(successMessage = "配置已自动保存") {
  const data = {};
  data.addr = currentConfig.addr || "";
  data.open_window = true;
  data.open_browser = false;
  data.vision_prompt = currentConfig.vision_prompt || "";
  data.vision_enabled = visionCapabilityEnabled;
  syncClientKeyNames();
  data.client_api_key_entries = normalizeClientKeys(clientKeys);
  data.text_model_profiles = normalizeTextProfiles(textProfiles);
  data.active_text_profile_id = activeTextProfileId;
  data.vision_model_profiles = normalizeVisionProfiles(visionProfiles);
  data.active_vision_profile_id = activeVisionProfileId;
  data.model_profiles = [];
  data.active_model_profile_id = "";
  setStatus("保存中");
  const res = await fetch("/api/config", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(data)
  });
  if (!res.ok) {
    let detail = `HTTP ${res.status}`;
    try {
      const payload = await res.json();
      detail = payload?.error?.message || detail;
    } catch {
      detail = await res.text() || detail;
    }
    setStatus("保存失败");
    throw new Error(detail);
  }
  const payload = await res.json();
  currentConfig = payload?.config || {...currentConfig, ...data};
  setStatus("已保存");
  if (successMessage) {
    showToast(successMessage, "success");
  }
}

generateKey.addEventListener("click", async () => {
  generateKey.disabled = true;
  try {
    const res = await fetch("/api/key", {method: "POST"});
    if (!res.ok) throw new Error(`key ${res.status}`);
    const data = await res.json();
    clientKeys.push({
      name: clientKeyName.value.trim() || `客户端 ${clientKeys.length + 1}`,
      key: data.key
    });
    clientKeyName.value = "";
    renderClientKeys();
    setStatus("已生成");
    await persistConfig("已生成并保存客户端 Key");
  } catch (err) {
    console.error(err);
    setStatus("生成失败");
    showToast(`生成失败：${err.message || err}`, "error");
  } finally {
    generateKey.disabled = false;
  }
});

const clientConfigureActions = [
  {button: configureOpenCode, client: "opencode", name: "OpenCode"},
  {button: configureCodex, client: "codex", name: "Codex"},
  {button: configureClaudeCode, client: "claude-code", name: "Claude Code"}
];

clientConfigureActions.forEach(({button, client, name}) => {
  button?.addEventListener("click", () => {
    configureClient({button, client, name}).catch((err) => {
      console.error(err);
      showToast(`配置 ${name} 失败：${err.message || err}`, "error");
    });
  });
});

async function configureClient({button, client, name}) {
  if (button) button.disabled = true;
  try {
    const res = await fetch("/api/client/configure", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({client})
    });
    if (!res.ok) throw new Error(await readErrorMessage(res));
    const payload = await res.json();
    const path = payload?.path ? `：${payload.path}` : "";
    const launchText = payload?.started ? `并已启动 ${name}` : "";
    showToast(`已一键配置 ${name}${launchText}${path}`, "success");
    setStatus(`${name} 已配置${launchText}`);
  } finally {
    if (button) button.disabled = false;
  }
}

function renderClientKeys() {
  clientKeyList.innerHTML = "";
  if (clientKeys.length === 0) {
    const empty = document.createElement("div");
    empty.className = "key-empty";
    empty.textContent = "还没有客户端 Key。输入名称后点击“生成 Key”。";
    clientKeyList.appendChild(empty);
    renderOpenCodeSnippet();
    renderOverview();
    return;
  }
  clientKeys.forEach((entry, index) => {
    const row = document.createElement("div");
    row.className = "key-item";
    row.innerHTML = `
      <input class="key-name" value="${escapeAttr(entry.name)}" aria-label="客户端名称">
      <code class="key-value" title="${escapeAttr(entry.key)}">${escapeHTML(entry.key)}</code>
      <button class="secondary small-action" type="button" data-action="copy">复制</button>
      <button class="danger" type="button">删除</button>
    `;
    row.querySelector(".key-name").addEventListener("input", (event) => {
      clientKeys[index].name = event.target.value;
    });
    row.querySelector(".key-name").addEventListener("change", () => {
      persistConfig("客户端名称已保存").catch((err) => {
        console.error(err);
        showToast(`保存失败：${err.message || err}`, "error");
      });
    });
    row.querySelector('[data-action="copy"]').addEventListener("click", () => {
      copyClientKey(entry.key).catch((err) => {
        console.error(err);
        showToast(`复制失败：${err.message || err}`, "error");
      });
    });
    row.querySelector(".danger").addEventListener("click", () => {
      clientKeys.splice(index, 1);
      renderClientKeys();
      setStatus("已删除");
      persistConfig("已删除并保存客户端 Key").catch((err) => {
        console.error(err);
        showToast(`保存失败：${err.message || err}`, "error");
      });
    });
    clientKeyList.appendChild(row);
  });
  renderOpenCodeSnippet();
  renderOverview();
}

async function loadLogs(page = currentLogPage) {
  const pageSize = Number(logPageSize.value || 20);
  const res = await fetch(`/api/logs?page=${encodeURIComponent(page)}&page_size=${encodeURIComponent(pageSize)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const payload = await res.json();
  currentLogPage = Number(payload.page || page || 1);
  currentLogTotal = Number(payload.total || 0);
  renderLogs(Array.isArray(payload.logs) ? payload.logs : []);
  renderLogPager();
}

function renderLogs(logs) {
  logList.innerHTML = "";
  if (logs.length === 0) {
    const empty = document.createElement("div");
    empty.className = "key-empty";
    empty.textContent = "暂无使用日志。客户端发起请求后会显示在这里。";
    logList.appendChild(empty);
    return;
  }
  logs.forEach((log) => {
    const item = document.createElement("div");
    item.className = "log-item";
    item.innerHTML = `
      <div class="log-head">
        <div>
          <strong>${escapeHTML(log.protocol || "-")}</strong>
          <span>${escapeHTML(new Date(log.at).toLocaleString())} · ${escapeHTML(log.method || "")} ${escapeHTML(log.path || "")}</span>
        </div>
        <em class="${Number(log.status) >= 400 ? "log-status error" : "log-status"}">${escapeHTML(String(log.status || "-"))}</em>
      </div>
      <div class="log-metrics">
        <span>令牌：${escapeHTML(log.client_name || "-")}</span>
        <span>上游：${escapeHTML(formatUpstream(log))}</span>
        <span>模型：${escapeHTML(log.model || "-")}</span>
        <span>输入：${formatNumber(log.input_tokens)} tok</span>
        <span>输出：${formatNumber(log.output_tokens)} tok</span>
        <span>总计：${formatNumber(log.total_tokens)} tok</span>
        <span>缓存命中：${formatNumber(log.cache_hit_tokens)} tok</span>
        <span>首 token：${formatDuration(log.first_token_ms)}</span>
        <span>耗时：${formatNumber(log.duration_ms)} ms</span>
      </div>
    `;
    logList.appendChild(item);
  });
}

function formatUpstream(log) {
  const name = String(log.upstream_name || "").trim();
  const provider = String(log.upstream_provider || "").trim();
  if (name && provider) return `${name} / ${provider}`;
  return name || provider || "-";
}

function formatDuration(value) {
  const n = Number(value || 0);
  return n > 0 ? `${formatNumber(n)} ms` : "-";
}

function renderLogPager() {
  const pageSize = Number(logPageSize.value || 20);
  const totalPages = Math.max(1, Math.ceil(currentLogTotal / pageSize));
  if (currentLogPage > totalPages) {
    currentLogPage = totalPages;
  }
  logPageInfo.textContent = `第 ${currentLogPage} / ${totalPages} 页，共 ${formatNumber(currentLogTotal)} 条`;
  prevLogPage.disabled = currentLogPage <= 1;
  nextLogPage.disabled = currentLogPage >= totalPages;
}

function formatNumber(value) {
  return Number(value || 0).toLocaleString();
}

async function copyClientKey(key) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(key);
  } else {
    const input = document.createElement("textarea");
    input.value = key;
    input.setAttribute("readonly", "");
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.appendChild(input);
    input.select();
    document.execCommand("copy");
    input.remove();
  }
  showToast("客户端 Key 已复制", "success");
}

function renderTextProfiles() {
  renderProfileList(textProfileList, textProfiles, activeTextProfileId, "text");
  renderOpenCodeSnippet();
  renderOverview();
}

function renderVisionProfiles() {
  renderProfileList(visionProfileList, visionProfiles, activeVisionProfileId, "vision");
  renderOverview();
}

function renderOverview() {
  const textProfile = activeTextProfile();
  const visionProfile = activeVisionProfile();
  const keys = normalizeClientKeys(clientKeys);
  if (homeBaseURL) homeBaseURL.textContent = location.host;
  if (homeTextModel) homeTextModel.textContent = formatTextModelList(textProfile, "使用请求模型名");
  if (homeTextProvider) homeTextProvider.textContent = profileHeadline(textProfile, "text");
  if (homeVisionModel) homeVisionModel.textContent = visionProfile?.model || "未设置模型";
  if (homeVisionProvider) homeVisionProvider.textContent = profileHeadline(visionProfile, "vision");
  if (homeKeyCount) homeKeyCount.textContent = `${keys.length} 个`;
  if (homeTextProfile) homeTextProfile.textContent = profileDetail(textProfile, "text");
  if (homeVisionProfile) homeVisionProfile.textContent = profileDetail(visionProfile, "vision");
  if (!visionCapabilityEnabled) {
    if (homeVisionModel) homeVisionModel.textContent = "未开启";
    if (homeVisionProvider) homeVisionProvider.textContent = "仅使用文本模型";
    if (homeVisionProfile) homeVisionProfile.textContent = "关闭后不会调用视觉模型";
  }
  if (homeProxyState) homeProxyState.textContent = serviceState.textContent || "在线";
}

function profileHeadline(profile, kind) {
  if (!profile) return "-";
  const provider = profile.provider || "-";
  const name = profile.name || (kind === "text" ? "文本模型" : "视觉模型");
  return `${name} / ${provider}`;
}

function profileDetail(profile, kind) {
  if (!profile) return "-";
  const model = kind === "text" ? formatTextModelList(profile, "使用客户端请求模型名") : profile.model || "未设置模型";
  const wire = kind === "text" ? ` · ${formatWireAPI(profile.wire_api)}` : "";
  return `${profile.name || "未命名"} · ${profile.provider || "-"} · ${model}${wire}`;
}

function renderProfileList(container, profiles, activeId, kind) {
  container.innerHTML = "";
  profiles.forEach((profile) => {
    const row = document.createElement("div");
    row.className = `profile-row${profile.id === activeId ? " active" : ""}`;
    row.innerHTML = `
      <button class="profile-main" type="button">
        <div>
          <strong>${escapeHTML(profile.name || "未命名")}</strong>
          <span>${escapeHTML(profileSummary(profile, kind))}</span>
        </div>
      </button>
      <div class="profile-actions">
        ${profile.id === activeId ? '<em class="profile-badge">当前</em>' : ""}
        <button class="secondary small-action" type="button" data-action="edit">编辑</button>
        <button class="danger small-action" type="button" data-action="delete">删除</button>
      </div>
    `;
    row.querySelector(".profile-main").addEventListener("click", () => {
      if (profile.id === activeId) return;
      if (kind === "text") {
        applyTextProfile(profile.id);
        renderTextProfiles();
        persistConfig(`已切换并保存文本模型：${profile.name || "未命名"}`).catch((err) => {
          console.error(err);
          showToast(`保存失败：${err.message || err}`, "error");
        });
      } else {
        applyVisionProfile(profile.id);
        renderVisionProfiles();
        persistConfig(`已切换并保存视觉模型：${profile.name || "未命名"}`).catch((err) => {
          console.error(err);
          showToast(`保存失败：${err.message || err}`, "error");
        });
      }
    });
    row.querySelector('[data-action="edit"]').addEventListener("click", () => {
      openProfileModal(kind, "edit", profile.id);
    });
    row.querySelector('[data-action="delete"]').addEventListener("click", () => {
      deleteProfile(kind, profile.id).catch((err) => {
        console.error(err);
        showToast(`保存失败：${err.message || err}`, "error");
      });
    });
    container.appendChild(row);
  });
}

async function deleteProfile(kind, id) {
  const isText = kind === "text";
  const profiles = isText ? textProfiles : visionProfiles;
  if (profiles.length <= 1) {
    showToast(isText ? "至少保留一个文本模型" : "至少保留一个视觉模型", "error");
    return;
  }
  const index = profiles.findIndex((profile) => profile.id === id);
  if (index < 0) return;
  profiles.splice(index, 1);
  if (isText) {
    if (activeTextProfileId === id) {
      activeTextProfileId = profiles[Math.max(0, index - 1)]?.id || profiles[0].id;
      applyTextProfile(activeTextProfileId);
    }
    renderTextProfiles();
    await persistConfig("已删除并保存文本模型");
  } else {
    if (activeVisionProfileId === id) {
      activeVisionProfileId = profiles[Math.max(0, index - 1)]?.id || profiles[0].id;
      applyVisionProfile(activeVisionProfileId);
    }
    renderVisionProfiles();
    await persistConfig("已删除并保存视觉模型");
  }
}

function openProfileModal(kind, mode, profileId = "") {
  profileModalKind = kind;
  profileModalMode = mode;
  const isText = kind === "text";
  const profiles = isText ? textProfiles : visionProfiles;
  const index = profiles.length + 1;
  const profile = mode === "edit"
    ? profiles.find((item) => item.id === profileId) || (isText ? activeTextProfile() : activeVisionProfile())
    : defaultProfileDraft(kind, index);
  profileModalEditId = profile?.id || "";
  profileModalTitle.textContent = modalTitle(kind, mode);
  profileModalHelp.textContent = mode === "edit"
    ? "修改后会更新该模型配置并自动保存。"
    : (isText ? "填写新的文本上游配置，创建后保存到列表。" : "填写新的视觉上游配置，创建后保存到列表。");
  profileModalSubmit.textContent = mode === "edit" ? "保存模型" : "创建模型";
  modalProfileName.value = profile?.name || (isText ? `文本模型 ${index}` : `视觉模型 ${index}`);
  modalProfileProvider.value = profile?.provider || "openai";
  modalProfileWireAPI.value = isText ? normalizeWireAPI(profile?.wire_api) : "chat_completions";
  modalProfileBaseURL.value = profile?.base_url || "";
  modalProfileAPIKey.value = profile?.api_key || "";
  modalProfileModelLabel.textContent = isText ? "强制模型名" : "模型名";
  modalProfileModel.placeholder = isText ? "可填多个，换行或逗号分隔；留空则使用客户端请求里的 model" : "例如 gpt-4o-mini";
  modalProfileModel.value = isText ? textProfileModels(profile).join("\n") : profile?.model || "";
  modalModelMappings = isText ? textProfileMappings(profile) : [];
  modalProfileModelWrap.hidden = isText;
  modelMappingSection.hidden = !isText;
  fetchModels.hidden = isText;
  renderModelMappingRows();
  modalProfileProxyWrap.hidden = !isText;
  modalProfileWireAPIWrap.hidden = !isText;
  modalProfileSupportsImagesWrap.hidden = !isText;
  modalProfileSupportsImages.checked = isText && profile?.supports_images === true;
  modalProfileProxyURL.value = isText ? profile?.proxy_url || "" : "";
  resetModelPicker();
  if (profileModal.showModal) {
    profileModal.showModal();
  } else {
    profileModal.setAttribute("open", "");
  }
  setTimeout(() => modalProfileName.select(), 0);
}

function closeModal() {
  if (profileModal.open && profileModal.close) {
    profileModal.close();
    return;
  }
  profileModal.removeAttribute("open");
}

async function fetchProviderModels() {
  const provider = modalProfileProvider.value;
  const baseURL = modalProfileBaseURL.value.trim();
  if (!baseURL) {
    throw new Error("请先填写 Base URL");
  }
  fetchedModels = [];
  renderFetchedModels();
  modelPickerPanel.hidden = false;
  modelPickerStatus.textContent = "正在获取模型...";
  fetchModels.disabled = true;
  if (fetchModelsForMapping) fetchModelsForMapping.disabled = true;
  const originalText = fetchModels.textContent;
  const originalMappingText = fetchModelsForMapping?.textContent || "";
  fetchModels.textContent = "获取中...";
  if (fetchModelsForMapping) fetchModelsForMapping.textContent = "获取中...";
  try {
    const res = await fetch("/api/models", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({
        provider,
        base_url: baseURL,
        api_key: modalProfileAPIKey.value.trim(),
        proxy_url: profileModalKind === "text" ? modalProfileProxyURL.value.trim() : ""
      })
    });
    if (!res.ok) {
      let detail = `HTTP ${res.status}`;
      try {
        const payload = await res.json();
        detail = payload?.error?.message || detail;
      } catch {
        detail = await res.text() || detail;
      }
      throw new Error(detail);
    }
    const payload = await res.json();
    fetchedModels = Array.isArray(payload.models) ? payload.models : [];
    renderFetchedModels();
    modelPickerStatus.textContent = fetchedModels.length
      ? `已获取 ${fetchedModels.length} 个模型，选择后点击“添加模型”。`
      : "没有获取到模型。";
    showToast(`已获取 ${fetchedModels.length} 个模型`, "success");
  } finally {
    fetchModels.disabled = false;
    fetchModels.textContent = originalText;
    if (fetchModelsForMapping) {
      fetchModelsForMapping.disabled = false;
      fetchModelsForMapping.textContent = originalMappingText;
    }
  }
}

function renderFetchedModels() {
  const keyword = modelSearch.value.trim().toLowerCase();
  modelSelect.innerHTML = "";
  const filtered = fetchedModels.filter((model) => {
    const id = String(model.id || "").toLowerCase();
    const name = String(model.name || "").toLowerCase();
    return !keyword || id.includes(keyword) || name.includes(keyword);
  });
  filtered.forEach((model) => {
    const option = document.createElement("option");
    option.value = model.id;
    option.textContent = model.name && model.name !== model.id ? `${model.id} · ${model.name}` : model.id;
    modelSelect.appendChild(option);
  });
  if (fetchedModels.length > 0) {
    modelPickerStatus.textContent = filtered.length
      ? `显示 ${filtered.length} / ${fetchedModels.length} 个模型。`
      : "没有匹配的模型。";
  }
}

function renderModelMappingRows() {
  if (!modelMappingRows) return;
  modelMappingRows.innerHTML = "";
  if (modalModelMappings.length === 0) {
    addModelMappingRow({name: "", model: "", context_window: ""}, false);
    return;
  }
  modalModelMappings.forEach((mapping, index) => {
    const row = document.createElement("div");
    row.className = "model-mapping-row";
    row.innerHTML = `
      <input data-field="name" value="${escapeAttr(mapping.name || "")}" placeholder="例如：DeepSeek V4 Flash">
      <input data-field="model" value="${escapeAttr(mapping.model || "")}" placeholder="例如：deepseek-v4-flash">
      <input data-field="context_window" type="number" min="0" step="1" value="${escapeAttr(mapping.context_window || "")}" placeholder="例如：128000">
      <button class="icon-button model-mapping-delete" type="button" aria-label="删除模型">×</button>
    `;
    row.querySelectorAll("input").forEach((input) => {
      input.addEventListener("input", () => {
        updateModelMappingFromRows();
      });
    });
    row.querySelector(".model-mapping-delete").addEventListener("click", () => {
      updateModelMappingFromRows();
      modalModelMappings.splice(index, 1);
      renderModelMappingRows();
    });
    modelMappingRows.appendChild(row);
  });
}

function addModelMappingRow(mapping = {}, focus = true) {
  updateModelMappingFromRows();
  modalModelMappings.push(normalizeModelMapping(mapping));
  renderModelMappingRows();
  if (focus) {
    const inputs = modelMappingRows.querySelectorAll(".model-mapping-row input");
    inputs[Math.max(0, inputs.length - 3)]?.focus();
  }
}

function updateModelMappingFromRows() {
  if (!modelMappingRows) return;
  modalModelMappings = [...modelMappingRows.querySelectorAll(".model-mapping-row")]
    .map((row) => normalizeModelMapping({
      name: row.querySelector('[data-field="name"]')?.value,
      model: row.querySelector('[data-field="model"]')?.value,
      context_window: row.querySelector('[data-field="context_window"]')?.value
    }));
}

function addSelectedModelsToModal(showMessage) {
  const selected = [...modelSelect.selectedOptions].map((option) => option.value);
  if (selected.length === 0 && modelSelect.value) {
    selected.push(modelSelect.value);
  }
  if (selected.length === 0) {
    modelPickerStatus.textContent = "请先在模型列表中选择要添加的模型。";
    return;
  }
  if (profileModalKind === "text") {
    updateModelMappingFromRows();
    const existing = new Set(modalModelMappings.map((mapping) => mapping.model || mapping.name).filter(Boolean));
    selected.forEach((model) => {
      if (!existing.has(model)) {
        modalModelMappings.push({name: model, model, context_window: ""});
        existing.add(model);
      }
    });
    renderModelMappingRows();
  } else {
    modalProfileModel.value = selected[0] || modalProfileModel.value;
  }
  if (showMessage && selected.length > 0) {
    showToast(`已选择 ${selected.length} 个模型`, "success");
  }
  updateSelectedModelStatus();
}

function updateSelectedModelStatus() {
  if (!modelSelect || fetchedModels.length === 0) return;
  const selectedCount = modelSelect.selectedOptions.length;
  if (selectedCount > 0) {
    modelPickerStatus.textContent = `已选择 ${selectedCount} 个模型，点击“添加模型”写入。`;
  }
}

function resetModelPicker() {
  fetchedModels = [];
  modelSearch.value = "";
  modelSelect.innerHTML = "";
  modelPickerPanel.hidden = true;
  modelPickerStatus.textContent = "点击模型即可填入。";
}

async function createProfileFromModal() {
  const isText = profileModalKind === "text";
  const isEdit = profileModalMode === "edit";
  if (isText) {
    updateModelMappingFromRows();
    const id = isEdit ? profileModalEditId : `text-${Date.now().toString(36)}`;
    const profile = normalizeTextProfile({
      id,
      name: modalProfileName.value,
      provider: modalProfileProvider.value,
      base_url: modalProfileBaseURL.value,
      api_key: modalProfileAPIKey.value,
      model_override: firstModelOverride(modelMappingsToModels(modalModelMappings)),
      model_overrides: modelMappingsToModels(modalModelMappings),
      model_mappings: normalizeModelMappings(modalModelMappings),
      wire_api: modalProfileWireAPI.value,
      supports_images: modalProfileSupportsImages.checked,
      proxy_url: modalProfileProxyURL.value
    }, textProfiles.length);
    if (isEdit) {
      replaceProfile(textProfiles, profile);
      if (activeTextProfileId === profile.id) {
        applyTextProfile(profile.id);
      }
    } else {
      textProfiles.push(profile);
    }
    renderTextProfiles();
    showPage("text");
    await persistConfig(isEdit ? "已更新并保存文本模型" : "已新增并保存文本模型");
  } else {
    const id = isEdit ? profileModalEditId : `vision-${Date.now().toString(36)}`;
    const profile = normalizeVisionProfile({
      id,
      name: modalProfileName.value,
      provider: modalProfileProvider.value,
      base_url: modalProfileBaseURL.value,
      api_key: modalProfileAPIKey.value,
      model: modalProfileModel.value
    }, visionProfiles.length);
    if (isEdit) {
      replaceProfile(visionProfiles, profile);
      if (activeVisionProfileId === profile.id) {
        applyVisionProfile(profile.id);
      }
    } else {
      visionProfiles.push(profile);
    }
    renderVisionProfiles();
    showPage("vision");
    await persistConfig(isEdit ? "已更新并保存视觉模型" : "已新增并保存视觉模型");
  }
  closeModal();
  profileModalForm.reset();
}

function modalTitle(kind, mode) {
  if (kind === "text") return mode === "edit" ? "编辑文本模型" : "新增文本模型";
  return mode === "edit" ? "编辑视觉模型" : "新增视觉模型";
}

function defaultProfileDraft(kind, index) {
  if (kind === "text") {
    return {
      id: "",
      name: `文本模型 ${index}`,
      provider: "openai",
      base_url: "",
      api_key: "",
      model_override: "",
      model_overrides: [],
      wire_api: "chat_completions",
      supports_images: false,
      proxy_url: ""
    };
  }
  return {
    id: "",
    name: `视觉模型 ${index}`,
    provider: "openai",
    base_url: "",
    api_key: "",
    model: ""
  };
}

function replaceProfile(profiles, profile) {
  const index = profiles.findIndex((item) => item.id === profile.id);
  if (index >= 0) {
    profiles[index] = profile;
  }
}

function activeTextProfile() {
  return textProfiles.find((profile) => profile.id === activeTextProfileId);
}

function activeVisionProfile() {
  return visionProfiles.find((profile) => profile.id === activeVisionProfileId);
}

function textProfileModels(profile) {
  if (!profile) return [];
  return modelMappingsToModels(textProfileMappings(profile));
}

function textProfileDisplayModels(profile) {
  return textProfileMappings(profile).map((mapping) => mapping.name || mapping.model).filter(Boolean);
}

function textProfileMappings(profile) {
  if (!profile) return [];
  return normalizeModelMappings(profile.model_mappings || profile.text_model_mappings || profile.model_overrides || profile.model_override);
}

function modelsForSnippet(profile) {
  const models = textProfileDisplayModels(profile);
  return models.length ? models : ["z-ai/glm-5.2"];
}

function formatTextModelList(profile, fallback) {
  const models = textProfileDisplayModels(profile);
  if (models.length === 0) return fallback;
  if (models.length === 1) return models[0];
  return `${models[0]} 等 ${models.length} 个模型`;
}

function parseModelOverrides(value) {
  const values = Array.isArray(value) ? value : String(value || "").split(/[\n,，;；]+/);
  const seen = new Set();
  return values
    .map((model) => String(model || "").trim())
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
}

function firstModelOverride(value) {
  return parseModelOverrides(value)[0] || "";
}

function normalizeModelMapping(value) {
  const name = String(value?.name || value?.display_name || "").trim();
  const model = String(value?.model || value || "").trim();
  const contextWindow = Number(value?.context_window || value?.contextWindow || 0);
  return {
    name: name || model,
    model: model || name,
    context_window: Number.isFinite(contextWindow) && contextWindow > 0 ? Math.floor(contextWindow) : 0
  };
}

function normalizeModelMappings(value) {
  const raw = Array.isArray(value)
    ? value
    : parseModelOverrides(value).map((model) => ({name: model, model}));
  const seen = new Set();
  return raw
    .map(normalizeModelMapping)
    .filter((mapping) => {
      const key = mapping.name || mapping.model;
      if (!mapping.model || !key || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
}

function modelMappingsToModels(mappings) {
  const seen = new Set();
  return normalizeModelMappings(mappings)
    .map((mapping) => mapping.model)
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
}

function visionInputModalities() {
  return relayImageInputEnabled() ? ["text", "image"] : ["text"];
}

function relayImageInputEnabled() {
  return activeTextProfile()?.supports_images === true || visionCapabilityEnabled;
}

function renderOpenCodeSnippet() {
  const profile = activeTextProfile();
  const models = textProfileModels(profile);
  const model = (models[0] || "z-ai/glm-5.2").trim();
  const key = normalizeClientKeys(clientKeys)[0]?.key || "请先生成客户端 Key";
  const inputModalities = visionInputModalities();
  if (opencodeConfig) {
    opencodeConfig.textContent = JSON.stringify({
      "$schema": "https://opencode.ai/config.json",
      provider: {
        "vision-relay": {
          npm: "@ai-sdk/openai-compatible",
          name: "Vision Relay",
          options: {
            baseURL: `${location.origin}/v1`,
            apiKey: key
          },
          models: Object.fromEntries(modelsForSnippet(profile).map((modelName) => [modelName, {
              name: modelName,
              attachment: relayImageInputEnabled(),
              attachments: relayImageInputEnabled(),
              vision: relayImageInputEnabled(),
              input_modalities: inputModalities,
              output_modalities: ["text"],
              modalities: {
                input: inputModalities,
                output: ["text"]
              }
            }]))
        }
      },
      model: `vision-relay/${model}`
    }, null, 2);
  }
  if (codexConfig) {
    const codexMappings = textProfileMappings(profile);
    const catalogMappings = codexMappings.length ? codexMappings : [{name: "z-ai/glm-5.2", model: "z-ai/glm-5.2", context_window: 0}];
    const codexDefaultModel = catalogMappings[0].name || catalogMappings[0].model;
    const codexCatalog = {
      models: catalogMappings.map((mapping, index) => {
        const slug = mapping.name || mapping.model;
        return {
        slug,
        display_name: slug,
        description: mapping.model && mapping.model !== slug
          ? `Vision Relay route to ${mapping.model}`
          : "Vision Relay model",
        base_instructions: "You are Codex, a coding agent. You and the user share the same workspace and collaborate to achieve the user's goals.",
        context_window: mapping.context_window || 128000,
        max_context_window: mapping.context_window || 128000,
        effective_context_window_percent: 95,
        default_reasoning_level: "high",
        default_reasoning_summary: "none",
        supported_reasoning_levels: [
          {effort: "none", description: "Disable reasoning"},
          {effort: "high", description: "Enable reasoning"}
        ],
        visibility: "list",
        supported_in_api: true,
        priority: 1000 + index,
        shell_type: "shell_command",
        input_modalities: inputModalities,
        supports_parallel_tool_calls: false,
        supports_image_detail_original: relayImageInputEnabled(),
        supports_reasoning_summaries: true,
        supports_search_tool: false,
        support_verbosity: false,
        truncation_policy: {mode: "bytes", limit: 10000},
        additional_speed_tiers: [],
        service_tiers: [],
        availability_nux: null,
        upgrade: null,
        experimental_supported_tools: []
      };
      })
    };
    codexConfig.textContent = [
      `# %USERPROFILE%\\.codex\\config.toml`,
      `model_provider = "custom"`,
      `model = "${codexDefaultModel}"`,
      `disable_response_storage = true`,
      `model_reasoning_effort = "high"`,
      `model_catalog_json = "vision-relay-model.json"`,
      `web_search = "disabled"`,
      ``,
      `[model_providers.custom]`,
      `name = "Vision Relay"`,
      `wire_api = "responses"`,
      `requires_openai_auth = true`,
      `base_url = "${location.origin}/v1"`,
      `experimental_bearer_token = "PROXY_MANAGED"`,
      ``,
      `[windows]`,
      `sandbox = "unelevated"`,
      ``,
      `# %USERPROFILE%\\.codex\\vision-relay-model.json`,
      JSON.stringify(codexCatalog, null, 2),
      ``,
      `# 当前项目 .codex\\config.toml`,
      `model_provider = "custom"`,
      `model = "${codexDefaultModel}"`,
      `disable_response_storage = true`,
      `model_reasoning_effort = "high"`,
      `model_catalog_json = "vision-relay-model.json"`,
      `web_search = "disabled"`,
      ``,
      `[model_providers.custom]`,
      `name = "Vision Relay"`,
      `wire_api = "responses"`,
      `requires_openai_auth = true`,
      `base_url = "${location.origin}/v1"`,
      `experimental_bearer_token = "PROXY_MANAGED"`,
      ``,
      `[windows]`,
      `sandbox = "unelevated"`
    ].join("\n");
  }
  if (claudeCodeConfig) {
    claudeCodeConfig.textContent = [
      `# PowerShell`,
      `$env:ANTHROPIC_BASE_URL = "${location.origin}"`,
      `$env:ANTHROPIC_AUTH_TOKEN = "${key}"`,
      ``,
      `# Claude Code 会请求：`,
      `${location.origin}/v1/messages`,
      `${location.origin}/v1/messages/count_tokens`,
      ``,
      `# 模型名可继续填写客户端里的 Claude 模型名；`,
      `# 如果在文本模型中设置了强制模型名，则最终会转发到当前文本模型：${model}`
    ].join("\n");
  }
}

function applyTextProfile(id) {
  const profile = textProfiles.find((item) => item.id === id) || textProfiles[0];
  if (!profile) return;
  activeTextProfileId = profile.id;
}

function applyVisionProfile(id) {
  const profile = visionProfiles.find((item) => item.id === id) || visionProfiles[0];
  if (!profile) return;
  activeVisionProfileId = profile.id;
}

function syncActiveTextProfileFromForm() {
}

function syncActiveVisionProfileFromForm() {
}

function migrateProfiles(cfg) {
  if (Array.isArray(cfg.model_profiles) && cfg.model_profiles.length > 0) {
    return {
      textProfiles: cfg.model_profiles.map((profile, index) => normalizeTextProfile({
        id: `text-${profile.id || index + 1}`,
        name: profile.name,
        provider: profile.text_provider,
        base_url: profile.text_base_url,
        api_key: profile.text_api_key,
        model_override: profile.text_model_override,
        model_overrides: profile.text_model_overrides || profile.model_overrides,
        model_mappings: profile.text_model_mappings || profile.model_mappings,
        wire_api: profile.text_wire_api,
        supports_images: profile.text_supports_images,
        proxy_url: profile.proxy_url
      }, index)),
      visionProfiles: cfg.model_profiles.map((profile, index) => normalizeVisionProfile({
        id: `vision-${profile.id || index + 1}`,
        name: profile.name,
        provider: profile.vision_provider,
        base_url: profile.vision_base_url,
        api_key: profile.vision_api_key,
        model: profile.vision_model
      }, index)),
      activeTextProfileId: cfg.active_model_profile_id ? `text-${cfg.active_model_profile_id}` : "",
      activeVisionProfileId: cfg.active_model_profile_id ? `vision-${cfg.active_model_profile_id}` : ""
    };
  }
  return {
    textProfiles: [textProfileFromConfig(cfg, "text-default", "默认文本模型")],
    visionProfiles: [visionProfileFromConfig(cfg, "vision-default", "默认视觉模型")]
  };
}

function textProfileFromConfig(cfg, id, name) {
  return normalizeTextProfile({
    id,
    name,
    provider: cfg.text_provider,
    base_url: cfg.text_base_url,
    api_key: cfg.text_api_key,
    model_override: cfg.text_model_override,
    model_overrides: cfg.text_model_overrides,
    model_mappings: cfg.text_model_mappings,
    wire_api: cfg.text_wire_api,
    supports_images: cfg.text_supports_images,
    proxy_url: cfg.proxy_url
  }, 0);
}

function visionProfileFromConfig(cfg, id, name) {
  return normalizeVisionProfile({
    id,
    name,
    provider: cfg.vision_provider,
    base_url: cfg.vision_base_url,
    api_key: cfg.vision_api_key,
    model: cfg.vision_model
  }, 0);
}

function textProfileFromForm(id, name) {
  return normalizeTextProfile({
    id,
    name,
    provider: "openai",
    base_url: "https://api.openai.com",
    api_key: "",
    model_override: "",
    model_overrides: [],
    model_mappings: [],
    wire_api: "chat_completions",
    supports_images: false,
    proxy_url: ""
  }, 0);
}

function visionProfileFromForm(id, name) {
  return normalizeVisionProfile({
    id,
    name,
    provider: "openai",
    base_url: "https://api.openai.com",
    api_key: "",
    model: "gpt-4o-mini"
  }, 0);
}

function normalizeTextProfiles(profiles) {
  return normalizeProfileList(profiles, normalizeTextProfile, () => textProfileFromForm("text-default", "默认文本模型"));
}

function normalizeVisionProfiles(profiles) {
  return normalizeProfileList(profiles, normalizeVisionProfile, () => visionProfileFromForm("vision-default", "默认视觉模型"));
}

function normalizeProfileList(profiles, normalizer, fallback) {
  const seen = new Set();
  const normalized = profiles.map((profile, index) => normalizer(profile, index)).filter((profile) => {
    if (!profile.id || seen.has(profile.id)) return false;
    seen.add(profile.id);
    return true;
  });
  return normalized.length ? normalized : [fallback()];
}

function normalizeTextProfile(profile, index) {
  const modelMappings = normalizeModelMappings(profile.model_mappings || profile.text_model_mappings || profile.model_overrides || profile.text_model_overrides || profile.model_override);
  const modelOverrides = modelMappingsToModels(modelMappings);
  return {
    id: String(profile.id || `text-${index + 1}`).trim(),
    name: String(profile.name || `文本模型 ${index + 1}`).trim(),
    provider: String(profile.provider || "openai").trim(),
    base_url: String(profile.base_url || "https://api.openai.com").trim(),
    api_key: String(profile.api_key || "").trim(),
    model_override: modelOverrides[0] || "",
    model_overrides: modelOverrides,
    model_mappings: modelMappings,
    wire_api: normalizeWireAPI(profile.wire_api),
    supports_images: profile.supports_images === true,
    proxy_url: String(profile.proxy_url || "").trim()
  };
}

function normalizeVisionProfile(profile, index) {
  return {
    id: String(profile.id || `vision-${index + 1}`).trim(),
    name: String(profile.name || `视觉模型 ${index + 1}`).trim(),
    provider: String(profile.provider || "openai").trim(),
    base_url: String(profile.base_url || "https://api.openai.com").trim(),
    api_key: String(profile.api_key || "").trim(),
    model: String(profile.model || "gpt-4o-mini").trim()
  };
}

function profileSummary(profile, kind) {
  const provider = profile.provider || "openai";
  const model = kind === "text" ? formatTextModelList(profile, "使用请求模型名") : profile.model || "未设置模型";
  const base = profile.base_url || "未设置 Base URL";
  const wire = kind === "text" ? ` · ${formatWireAPI(profile.wire_api)}` : "";
  return `${provider} · ${model}${wire} · ${base}`;
}

function normalizeWireAPI(value) {
  return String(value || "").trim().toLowerCase() === "responses" ? "responses" : "chat_completions";
}

function formatWireAPI(value) {
  return normalizeWireAPI(value) === "responses" ? "Responses" : "Chat Completions";
}

function defaultBaseURL(provider) {
  switch (String(provider || "openai").toLowerCase()) {
    case "anthropic":
      return "https://api.anthropic.com";
    case "gemini":
      return "https://generativelanguage.googleapis.com";
    case "ollama":
      return "http://127.0.0.1:11434";
    default:
      return "https://api.openai.com";
  }
}

function syncClientKeyNames() {
  [...clientKeyList.querySelectorAll(".key-item")].forEach((row, index) => {
    const input = row.querySelector(".key-name");
    if (clientKeys[index] && input) {
      clientKeys[index].name = input.value;
    }
  });
}

function normalizeClientKeys(entries) {
  const seen = new Set();
  return entries
    .map((entry, index) => ({
      name: String(entry.name || `客户端 ${index + 1}`).trim() || `客户端 ${index + 1}`,
      key: String(entry.key || "").trim()
    }))
    .filter((entry) => {
      if (!entry.key || seen.has(entry.key)) return false;
      seen.add(entry.key);
      return true;
    });
}

function keysToEntries(keys) {
  return keys.map((key, index) => ({name: `旧令牌 ${index + 1}`, key}));
}

segments.forEach((button) => {
  button.addEventListener("click", () => {
    testMode = button.dataset.mode;
    segments.forEach((item) => item.classList.toggle("active", item === button));
  });
});

imageInput.addEventListener("change", async () => {
  const file = imageInput.files[0];
  imageDataUrl = "";
  preview.innerHTML = "";
  fileName.textContent = "支持 data URL、本地上传、远程图片 URL";
  if (!file) return;
  imageDataUrl = await readAsDataURL(file);
  fileName.textContent = `${file.name} · ${formatBytes(file.size)}`;
  const img = document.createElement("img");
  img.src = imageDataUrl;
  preview.appendChild(img);
});

send.addEventListener("click", async () => {
  const prompt = document.querySelector("#prompt").value.trim() || "请描述这张图片。";
  const model = textProfileDisplayModels(activeTextProfile())[0] || "local-text-model";
  const headers = {"Content-Type": "application/json"};
  const localKey = firstLocalKey();
  if (localKey) {
    headers.Authorization = `Bearer ${localKey}`;
  }

  const request = testMode === "responses"
    ? buildResponsesRequest(model, prompt)
    : buildChatRequest(model, prompt);

  output.textContent = `POST ${request.path}\n\n请求中...`;
  visionOutput.textContent = imageDataUrl ? "等待图片模型返回..." : "本次请求未上传图片";
  if (imageDataUrl && !visionCapabilityEnabled) {
    visionOutput.textContent = "视觉模型能力未开启，本次请求直接发送到文本模型";
  }
  send.disabled = true;
  try {
    const res = await fetch(request.path, {
      method: "POST",
      headers,
      body: JSON.stringify(request.body)
    });
    const text = await res.text();
    try {
      output.textContent = JSON.stringify(JSON.parse(text), null, 2);
    } catch {
      output.textContent = text;
    }
    await refreshVisionDebug();
  } catch (err) {
    output.textContent = String(err);
    await refreshVisionDebug();
  } finally {
    send.disabled = false;
  }
});

async function refreshVisionDebug() {
  if (!visionCapabilityEnabled) {
    visionOutput.textContent = "视觉模型能力未开启，当前请求不会触发图片解析";
    return;
  }
  try {
    const res = await fetch("/api/debug/vision");
    if (!res.ok) return;
    const info = await res.json();
    if (!info || !info.at || info.image_count === 0) {
      visionOutput.textContent = "尚未触发图片解析";
      return;
    }
    const lines = [
      `时间：${new Date(info.at).toLocaleString()}`,
      `上游：${info.provider || "-"} / ${info.model || "-"}`,
      `图片数：${info.image_count}`,
      `用户需求：${info.user_text || "-"}`,
      "",
      info.error ? `错误：${info.error}` : info.text
    ];
    visionOutput.textContent = lines.join("\n");
  } catch (err) {
    console.error(err);
  }
}

function buildChatRequest(model, prompt) {
  const content = [{type: "text", text: prompt}];
  if (imageDataUrl) {
    content.push({type: "image_url", image_url: {url: imageDataUrl}});
  }
  return {
    path: "/v1/chat/completions",
    body: {
      model,
      messages: [{role: "user", content}],
      temperature: 0.2
    }
  };
}

function buildResponsesRequest(model, prompt) {
  const content = [{type: "input_text", text: prompt}];
  if (imageDataUrl) {
    content.push({type: "input_image", image_url: imageDataUrl});
  }
  return {
    path: "/v1/responses",
    body: {
      model,
      input: [{role: "user", content}],
      temperature: 0.2
    }
  };
}

function firstLocalKey() {
  syncClientKeyNames();
  return normalizeClientKeys(clientKeys)[0]?.key || "";
}

function escapeAttr(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function escapeHTML(value) {
  return escapeAttr(value);
}

function readAsDataURL(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

function formatBytes(value) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / Math.pow(1024, index)).toFixed(index ? 1 : 0)} ${units[index]}`;
}

loadConfig().catch((err) => {
  console.error(err);
  setStatus("加载失败");
  serviceState.textContent = "离线";
});
