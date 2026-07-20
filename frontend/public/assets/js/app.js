const form = document.querySelector("#configForm");
const statusEl = document.querySelector("#status");
const toast = document.querySelector("#toast");
const serviceState = document.querySelector("#serviceState");
const reloadConfig = document.querySelector("#reloadConfig");
const refreshLogs = document.querySelector("#refreshLogs");
const clearLogs = document.querySelector("#clearLogs");
const logList = document.querySelector("#logList");
const logPageSize = document.querySelector("#logPageSize");
const logPageInfo = document.querySelector("#logPageInfo");
const prevLogPage = document.querySelector("#prevLogPage");
const nextLogPage = document.querySelector("#nextLogPage");
const refreshDashboard = document.querySelector("#refreshDashboard");
const dashboardPeriods = [...document.querySelectorAll("[data-dashboard-period]")];
const dashboardChartModes = [...document.querySelectorAll("[data-dashboard-chart-mode]")];
const dashboardSupplier = document.querySelector("#dashboardSupplier");
const dashboardModel = document.querySelector("#dashboardModel");
const dashboardTokenChart = document.querySelector("#dashboardTokenChart");
const dashboardRequestChart = document.querySelector("#dashboardRequestChart");
const dashboardTokenLegend = document.querySelector("#dashboardTokenLegend");
const dashboardModelRows = document.querySelector("#dashboardModelRows");
const opencodeConfig = document.querySelector("#opencodeConfig");
const configureOpenCode = document.querySelector("#configureOpenCode");
const codexConfig = document.querySelector("#codexConfig");
const configureCodex = document.querySelector("#configureCodex");
const restoreCodex = document.querySelector("#restoreCodex");
const preserveCodexOfficialAuth = document.querySelector("#preserveCodexOfficialAuth");
const unifyCodexSessionHistory = document.querySelector("#unifyCodexSessionHistory");

const claudeCodeConfig = document.querySelector("#claudeCodeConfig");
const configureClaudeCode = document.querySelector("#configureClaudeCode");
const openclawConfig = document.querySelector("#openclawConfig");
const configureOpenClaw = document.querySelector("#configureOpenClaw");
const clientRouteInputs = {
  codex: document.querySelector("#routeCodex"),
  opencode: document.querySelector("#routeOpenCode"),
  "claude-code": document.querySelector("#routeClaudeCode"),
  openclaw: document.querySelector("#routeOpenClaw")
};
const settingsLocalAPIEnabled = document.querySelector("#settingsLocalAPIEnabled");
const localAPIWarning = document.querySelector("#localAPIWarning");
const settingsAPIHost = document.querySelector("#settingsAPIHost");
const settingsAPIPort = document.querySelector("#settingsAPIPort");
const saveProgramSettings = document.querySelector("#saveProgramSettings");
const detectClientPaths = document.querySelector("#detectClientPaths");
const clientPathDetectionState = document.querySelector("#clientPathDetectionState");
const clientConfigPathInputs = {
  codex: document.querySelector("#configPathCodex"),
  opencode: document.querySelector("#configPathOpenCode"),
  "claude-code": document.querySelector("#configPathClaudeCode"),
  "claude-cli": document.querySelector("#configPathClaudeCLI"),
  openclaw: document.querySelector("#configPathOpenClaw")
};
const clientProgramPathInputs = {
  codex: document.querySelector("#programPathCodex"),
  "codex-cli": document.querySelector("#programPathCodexCLI"),
  opencode: document.querySelector("#programPathOpenCode"),
  "claude-code": document.querySelector("#programPathClaudeCode"),
  "claude-cli": document.querySelector("#programPathClaudeCLI"),
  openclaw: document.querySelector("#programPathOpenClaw")
};
const clientAutoRestartInputs = {
  codex: document.querySelector("#autoRestartCodex"),
  "codex-cli": document.querySelector("#autoRestartCodexCLI"),
  opencode: document.querySelector("#autoRestartOpenCode"),
  "claude-code": document.querySelector("#autoRestartClaudeCode"),
  "claude-cli": document.querySelector("#autoRestartClaudeCLI"),
  openclaw: document.querySelector("#autoRestartOpenClaw")
};
const clientAutoStartInputs = {
  codex: document.querySelector("#autoStartCodex"),
  "codex-cli": document.querySelector("#autoStartCodexCLI"),
  opencode: document.querySelector("#autoStartOpenCode"),
  "claude-code": document.querySelector("#autoStartClaudeCode"),
  "claude-cli": document.querySelector("#autoStartClaudeCLI"),
  openclaw: document.querySelector("#autoStartOpenClaw")
};
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
const navItems = [...document.querySelectorAll(".nav-item")];
const pages = [...document.querySelectorAll("[data-page-panel]")];
const homeJumpButtons = [...document.querySelectorAll(".home-jump")];
const homeBaseURL = document.querySelector("#homeBaseURL");
const homeTextModel = document.querySelector("#homeTextModel");
const homeTextProvider = document.querySelector("#homeTextProvider");
const homeVisionModel = document.querySelector("#homeVisionModel");
const homeVisionProvider = document.querySelector("#homeVisionProvider");
const homeTextProfile = document.querySelector("#homeTextProfile");
const homeVisionProfile = document.querySelector("#homeVisionProfile");
const homeProxyState = document.querySelector("#homeProxyState");
const checkUpdateButton = document.querySelector("#checkUpdate");
const installUpdateButton = document.querySelector("#installUpdate");
const currentVersionEl = document.querySelector("#currentVersion");
const latestVersionEl = document.querySelector("#latestVersion");
const updatePublishedAt = document.querySelector("#updatePublishedAt");
const updateState = document.querySelector("#updateState");
const updateNotes = document.querySelector("#updateNotes");
const releaseLink = document.querySelector("#releaseLink");
const serviceCard = document.querySelector("#serviceCard");
const clientTabButtons = [...document.querySelectorAll("[data-client-tab]")];
const clientTabPanels = [...document.querySelectorAll("[data-client-panel]")];

let textProfiles = [];
let activeTextProfileId = "";
let visionProfiles = [];
let activeVisionProfileId = "";
let visionCapabilityEnabled = true;
let toastTimer = 0;
let currentConfig = {};
let clientRouteEnabled = normalizeClientRoutes({});
let programSettings = {
  addr: "127.0.0.1:8787",
  localAPIEnabled: true,
  openWindow: true,
  openBrowser: false,
  clientConfigPaths: {},
  clientProgramPaths: {},
  clientAutoRestart: normalizeClientBehavior({}, true),
  clientAutoStart: normalizeClientBehavior({}, false),
  clientPathsDetected: false
};
let profileModalKind = "text";
let profileModalMode = "create";
let profileModalEditId = "";
let profileDragState = null;
let fetchedModels = [];
let modalModelMappings = [];
let currentLogPage = 1;
let currentLogTotal = 0;
let dashboardPeriod = "day";
let dashboardChartMode = "type";
let dashboardSupplierFilter = "";
let dashboardModelFilter = "";
let dashboardPayload = null;
let dashboardRequestSequence = 0;
let dashboardRequestController = null;

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
    if (item.dataset.page === "dashboard") {
      loadDashboard().catch((err) => {
        console.error(err);
        showToast(`加载看板失败：${err.message || err}`, "error");
      });
    }
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

clientTabButtons.forEach((button) => {
  button.addEventListener("click", () => {
    const client = button.dataset.clientTab;
    clientTabButtons.forEach((tab) => {
      const active = tab.dataset.clientTab === client;
      tab.classList.toggle("active", active);
      tab.setAttribute("aria-selected", String(active));
    });
    clientTabPanels.forEach((panel) => {
      const active = panel.dataset.clientPanel === client;
      panel.classList.toggle("active", active);
      panel.hidden = !active;
    });
  });
});

function showPage(page) {
  navItems.forEach((item) => item.classList.toggle("active", item.dataset.page === page));
  pages.forEach((panel) => panel.classList.toggle("active", panel.dataset.pagePanel === page));
  window.scrollTo({top: 0, left: 0, behavior: "auto"});
}

function setStatus(text) {
  statusEl.textContent = text;
}

function setServiceOnline(online) {
  const localAPIEnabled = programSettings.localAPIEnabled !== false;
  const stateText = localAPIEnabled
    ? (online ? "本地 API 服务运行正常" : "本地 API 服务连接失败")
    : "本地 API 服务已关闭";
  serviceState.textContent = stateText;
  if (homeProxyState) homeProxyState.textContent = stateText;
  if (serviceCard) {
    serviceCard.classList.toggle("online", localAPIEnabled && online);
    serviceCard.classList.toggle("offline", localAPIEnabled && !online);
    serviceCard.classList.toggle("disabled", !localAPIEnabled);
  }
}

function showToast(message, type = "info") {
  if (window.VisionRelayUI?.notify) {
    window.VisionRelayUI.notify(message, type);
    return;
  }
  clearTimeout(toastTimer);
  toast.textContent = message;
  toast.className = `toast show ${type}`;
  toastTimer = setTimeout(() => {
    toast.className = "toast";
  }, 3200);
}

async function confirmAction(options) {
  if (window.VisionRelayUI?.confirm) {
    return await window.VisionRelayUI.confirm(options);
  }
  showToast("确认组件加载失败，请刷新页面后重试", "error");
  return false;
}

async function readErrorMessage(res) {
  try {
    const payload = await res.json();
    return payload?.error?.message || `HTTP ${res.status}`;
  } catch {
    return await res.text() || `HTTP ${res.status}`;
  }
}

function normalizeClientConfigPaths(paths) {
  return {
    codex: String(paths?.codex || "").trim(),
    opencode: String(paths?.opencode || "").trim(),
    "claude-code": String(paths?.["claude-code"] || "").trim(),
    "claude-cli": String(paths?.["claude-cli"] || "").trim(),
    openclaw: String(paths?.openclaw || "").trim()
  };
}

function normalizeClientProgramPaths(paths) {
  return {
    codex: String(paths?.codex || "").trim(),
    "codex-cli": String(paths?.["codex-cli"] || "").trim(),
    opencode: String(paths?.opencode || "").trim(),
    "claude-code": String(paths?.["claude-code"] || "").trim(),
    "claude-cli": String(paths?.["claude-cli"] || "").trim(),
    openclaw: String(paths?.openclaw || "").trim()
  };
}

function normalizeClientBehavior(values, fallback) {
  return {
    codex: typeof values?.codex === "boolean" ? values.codex : fallback,
    "codex-cli": typeof values?.["codex-cli"] === "boolean" ? values["codex-cli"] : fallback,
    opencode: typeof values?.opencode === "boolean" ? values.opencode : fallback,
    "claude-code": typeof values?.["claude-code"] === "boolean" ? values["claude-code"] : fallback,
    "claude-cli": typeof values?.["claude-cli"] === "boolean" ? values["claude-cli"] : fallback,
    openclaw: typeof values?.openclaw === "boolean" ? values.openclaw : fallback
  };
}

function splitListenAddress(value) {
  const address = String(value || "").trim();
  if (address.startsWith("[")) {
    const bracket = address.indexOf("]");
    if (bracket >= 0 && address[bracket + 1] === ":") {
      return {host: address.slice(1, bracket), port: address.slice(bracket + 2)};
    }
  }
  const separator = address.lastIndexOf(":");
  if (separator >= 0) {
    return {host: address.slice(0, separator), port: address.slice(separator + 1)};
  }
  return {host: "127.0.0.1", port: "8787"};
}

function joinListenAddress(host, port) {
  const normalizedHost = String(host || "").trim().replace(/^\[|\]$/g, "");
  return normalizedHost.includes(":")
    ? `[${normalizedHost}]:${port}`
    : `${normalizedHost}:${port}`;
}

function syncLocalAPIWarning() {
  const disabled = settingsLocalAPIEnabled?.checked === false;
  if (localAPIWarning) localAPIWarning.hidden = !disabled;
}

function syncProgramSettingsInputs() {
  const address = splitListenAddress(programSettings.addr);
  if (settingsLocalAPIEnabled) settingsLocalAPIEnabled.checked = programSettings.localAPIEnabled;
  syncLocalAPIWarning();
  if (settingsAPIHost) settingsAPIHost.value = address.host;
  if (settingsAPIPort) settingsAPIPort.value = address.port || "8787";
  Object.entries(clientConfigPathInputs).forEach(([client, input]) => {
    if (input) input.value = programSettings.clientConfigPaths[client] || "";
  });
  Object.entries(clientProgramPathInputs).forEach(([client, input]) => {
    if (input) input.value = programSettings.clientProgramPaths[client] || "";
  });
  Object.entries(clientAutoRestartInputs).forEach(([client, input]) => {
    if (input) input.checked = programSettings.clientAutoRestart[client] !== false;
  });
  Object.entries(clientAutoStartInputs).forEach(([client, input]) => {
    if (input) input.checked = programSettings.clientAutoStart[client] === true;
  });
  if (clientPathDetectionState) {
    clientPathDetectionState.textContent = programSettings.clientPathsDetected ? "\u5df2\u5b8c\u6210\u68c0\u6d4b" : "\u5c1a\u672a\u68c0\u6d4b";
  }
}

function collectClientPaths(inputs) {
  return Object.fromEntries(Object.entries(inputs).map(([client, input]) => [client, input?.value.trim() || ""]));
}

function collectClientBehavior(inputs) {
  return Object.fromEntries(Object.entries(inputs).map(([client, input]) => [client, input?.checked === true]));
}

async function loadConfig() {
  const res = await fetch("/api/config");
  if (!res.ok) throw new Error(`config ${res.status}`);
  const cfg = await res.json();
  currentConfig = cfg;
  programSettings = {
    addr: cfg.addr || "127.0.0.1:8787",
    localAPIEnabled: cfg.local_api_enabled !== false,
    openWindow: cfg.open_window !== false,
    openBrowser: cfg.open_browser === true,
    clientConfigPaths: normalizeClientConfigPaths(cfg.client_config_paths),
    clientProgramPaths: normalizeClientProgramPaths(cfg.client_program_paths),
    clientAutoRestart: normalizeClientBehavior(cfg.client_auto_restart, true),
    clientAutoStart: normalizeClientBehavior(cfg.client_auto_start, false),
    clientPathsDetected: cfg.client_paths_detected === true
  };
  syncProgramSettingsInputs();
  clientRouteEnabled = normalizeClientRoutes(cfg.client_route_enabled);
  syncClientRouteInputs();
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
  if (preserveCodexOfficialAuth) {
    preserveCodexOfficialAuth.checked = cfg.preserve_codex_official_auth_on_switch !== false;
  }
  if (unifyCodexSessionHistory) {
    unifyCodexSessionHistory.checked = cfg.unify_codex_session_history === true;
  }
  renderTextProfiles();
  applyTextProfile(activeTextProfileId);
  renderVisionProfiles();
  applyVisionProfile(activeVisionProfileId);
  setServiceOnline(true);
  renderOverview();
  setStatus("已加载");
}

reloadConfig.addEventListener("click", () => {
  loadConfig().catch((err) => {
    console.error(err);
    setStatus("加载失败");
    setServiceOnline(false);
  });
});

refreshDashboard.addEventListener("click", () => {
  loadDashboard().catch((err) => {
    console.error(err);
    showToast(`加载看板失败：${err.message || err}`, "error");
  });
});

dashboardPeriods.forEach((button) => {
  button.addEventListener("click", () => {
    dashboardPeriod = button.dataset.dashboardPeriod || "day";
    dashboardPeriods.forEach((item) => item.classList.toggle("active", item === button));
    loadDashboard().catch((err) => {
      console.error(err);
      showToast(`加载看板失败：${err.message || err}`, "error");
    });
  });
});

dashboardChartModes.forEach((button) => {
  button.addEventListener("click", () => {
    dashboardChartMode = button.dataset.dashboardChartMode || "type";
    dashboardChartModes.forEach((item) => item.classList.toggle("active", item === button));
    if (dashboardPayload) renderDashboardTokenTrend(dashboardPayload);
  });
});

dashboardSupplier.addEventListener("change", () => {
  dashboardSupplierFilter = dashboardSupplier.value;
  dashboardModelFilter = "";
  loadDashboard().catch((err) => {
    console.error(err);
    showToast(`加载看板失败：${err.message || err}`, "error");
  });
});

dashboardModel.addEventListener("change", () => {
  dashboardModelFilter = dashboardModel.value;
  loadDashboard().catch((err) => {
    console.error(err);
    showToast(`加载看板失败：${err.message || err}`, "error");
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

preserveCodexOfficialAuth?.addEventListener("change", () => {
	const enabled = preserveCodexOfficialAuth.checked;
  const message = preserveCodexOfficialAuth.checked ? "切换第三方时将保留官方登录" : "切换第三方时将使用中转认证";
  renderOpenCodeSnippet();
  persistConfig(message).catch((err) => {
    console.error(err);
		preserveCodexOfficialAuth.checked = !enabled;
    renderOpenCodeSnippet();
    showToast(`保存失败：${err.message || err}`, "error");
  });
});

unifyCodexSessionHistory?.addEventListener("change", () => {
	const enabled = unifyCodexSessionHistory.checked;
	updateCodexHistorySetting(enabled).catch((err) => {
    console.error(err);
		if (!err.codexSettingPersisted) {
			unifyCodexSessionHistory.checked = !enabled;
		}
		const prefix = err.codexSettingPersisted ? "开关已保存，但 Codex 会话历史处理失败" : "更新 Codex 会话历史失败";
		showToast(`${prefix}：${err.message || err}`, "error");
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
  data.addr = programSettings.addr;
  data.local_api_enabled = programSettings.localAPIEnabled;
  data.client_config_paths = normalizeClientConfigPaths(programSettings.clientConfigPaths);
  data.client_program_paths = normalizeClientProgramPaths(programSettings.clientProgramPaths);
  data.client_auto_restart = normalizeClientBehavior(programSettings.clientAutoRestart, true);
  data.client_auto_start = normalizeClientBehavior(programSettings.clientAutoStart, false);
  data.client_paths_detected = programSettings.clientPathsDetected;
  data.open_window = programSettings.openWindow;
  data.open_browser = programSettings.openBrowser;
  data.vision_prompt = currentConfig.vision_prompt || "";
  data.vision_enabled = visionCapabilityEnabled;
  data.preserve_codex_official_auth_on_switch = preserveCodexOfficialAuth?.checked !== false;
  data.unify_codex_session_history = unifyCodexSessionHistory?.checked === true;
  data.client_route_enabled = normalizeClientRoutes(clientRouteEnabled);
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
  setServiceOnline(true);
  setStatus("已保存");
  if (successMessage) {
    showToast(successMessage, "success");
  }
}

settingsLocalAPIEnabled?.addEventListener("change", () => {
  syncLocalAPIWarning();
  if (!settingsLocalAPIEnabled.checked) {
    showToast("关闭本地服务后视觉模型将不可用；未勾选多模态的文本模型将无法实现图片识别。", "warning");
  }
});

saveProgramSettings?.addEventListener("click", async () => {
  const port = Number(settingsAPIPort?.value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    showToast("API \u7aef\u53e3\u5fc5\u987b\u662f 1 \u5230 65535 \u4e4b\u95f4\u7684\u6574\u6570", "error");
    settingsAPIPort?.focus();
    return;
  }
  const previousAddress = programSettings.addr;
  const previousLocalAPIEnabled = programSettings.localAPIEnabled;
  programSettings = {
    ...programSettings,
    addr: joinListenAddress(settingsAPIHost?.value, port),
    localAPIEnabled: settingsLocalAPIEnabled?.checked !== false,
    clientConfigPaths: collectClientPaths(clientConfigPathInputs),
    clientProgramPaths: collectClientPaths(clientProgramPathInputs),
    clientAutoRestart: collectClientBehavior(clientAutoRestartInputs),
    clientAutoStart: collectClientBehavior(clientAutoStartInputs),
    clientPathsDetected: true
  };
  saveProgramSettings.disabled = true;
  let settingsSaved = false;
  const localAPIModeChanged = previousLocalAPIEnabled !== programSettings.localAPIEnabled;
  try {
    await persistConfig("");
    settingsSaved = true;
    const updatedClients = localAPIModeChanged ? await applyEnabledClientRoutes() : [];
    syncProgramSettingsInputs();
    renderOpenCodeSnippet();
    const restartRequired = previousAddress !== programSettings.addr;
    const clientNames = updatedClients.map((client) => client.name || client.client).filter(Boolean).join("、");
    if (!programSettings.localAPIEnabled) {
      const routeMessage = clientNames ? `已将 ${clientNames} 改为直连供应商，请重启客户端程序。` : "";
      showToast(`设置已保存。${routeMessage}关闭本地服务后视觉模型将不可用；未勾选多模态的文本模型将无法实现图片识别。`, "warning");
    } else if (localAPIModeChanged && clientNames) {
      const restartMessage = restartRequired ? "API 地址或端口将在重启 Vision Relay 后生效；" : "";
      showToast(`设置已保存；${restartMessage}已将 ${clientNames} 接入本地 API，请重启客户端程序`, "success");
    } else {
      showToast(restartRequired
        ? "\u8bbe\u7f6e\u5df2\u4fdd\u5b58\uff1bAPI \u5730\u5740\u6216\u7aef\u53e3\u5c06\u5728\u91cd\u542f Vision Relay \u540e\u751f\u6548"
        : "\u7a0b\u5e8f\u8bbe\u7f6e\u5df2\u4fdd\u5b58", "success");
    }
    setStatus(restartRequired ? "\u8bbe\u7f6e\u5df2\u4fdd\u5b58\uff0c\u7b49\u5f85\u91cd\u542f\u751f\u6548" : "\u7a0b\u5e8f\u8bbe\u7f6e\u5df2\u4fdd\u5b58");
  } catch (err) {
    console.error(err);
    const prefix = settingsSaved && localAPIModeChanged
      ? "设置已保存，但同步客户端路由失败"
      : "保存程序设置失败";
    showToast(`${prefix}：${err.message || err}`, "error");
  } finally {
    saveProgramSettings.disabled = false;
  }
});

detectClientPaths?.addEventListener("click", async () => {
  detectClientPaths.disabled = true;
  if (clientPathDetectionState) clientPathDetectionState.textContent = "\u6b63\u5728\u68c0\u6d4b";
  try {
    const res = await fetch("/api/settings/detect-clients", {method: "POST"});
    if (!res.ok) throw new Error(await readErrorMessage(res));
    await loadConfig();
    showToast("\u5ba2\u6237\u7aef\u914d\u7f6e\u6587\u4ef6\u548c\u7a0b\u5e8f\u4f4d\u7f6e\u5df2\u91cd\u65b0\u68c0\u6d4b", "success");
    setStatus("\u5ba2\u6237\u7aef\u4f4d\u7f6e\u68c0\u6d4b\u5b8c\u6210");
  } catch (err) {
    console.error(err);
    if (clientPathDetectionState) clientPathDetectionState.textContent = "\u68c0\u6d4b\u5931\u8d25";
    showToast(`\u91cd\u65b0\u68c0\u6d4b\u5ba2\u6237\u7aef\u5931\u8d25\uff1a${err.message || err}`, "error");
  } finally {
    detectClientPaths.disabled = false;
  }
});

const clientConfigureActions = [
  {button: configureOpenCode, client: "opencode", name: "OpenCode"},
  {button: configureCodex, client: "codex", name: "Codex"},
  {button: configureClaudeCode, client: "claude-code", name: "Claude"},
  {button: configureOpenClaw, client: "openclaw", name: "OpenClaw"}
];

clientConfigureActions.forEach(({button, client, name}) => {
  button?.addEventListener("click", () => {
    configureClient({button, client, name}).catch((err) => {
      console.error(err);
      showToast(`配置 ${name} 失败：${err.message || err}`, "error");
    });
  });
});

clientConfigureActions.forEach(({client, name}) => {
  clientRouteInputs[client]?.addEventListener("change", () => {
    updateClientRouteSetting(client, name).catch((err) => {
      console.error(err);
      clientRouteEnabled[client] = !clientRouteInputs[client].checked;
      syncClientRouteInputs();
      showToast(`保存 ${name} 路由开关失败：${err.message || err}`, "error");
    });
  });
});

restoreCodex?.addEventListener("click", () => {
  restoreCodexOfficialMode().catch((err) => {
    console.error(err);
    showToast(`恢复 Codex 官方模式失败：${err.message || err}`, "error");
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
    const programResults = Array.isArray(payload?.programs) ? payload.programs : [];
    const programRestarted = programResults.some((program) => program?.restarted === true);
    const programStarted = programResults.some((program) => program?.started === true);
    const programRestartRequired = programResults.some((program) => program?.restart_required === true);
    const programWasRunning = programResults.some((program) => program?.was_running === true);
    const warnings = programResults.map((program) => program?.program_warning).filter(Boolean);
    if (programResults.length === 0 && payload?.program_warning) warnings.push(payload.program_warning);
    clientRouteEnabled[client] = payload?.route_enabled !== false;
    await loadConfig();
    const path = payload?.path ? `：${payload.path}` : "";
    let behaviorMessage = "配置已写入";
    if (programRestarted || payload?.restarted === true) {
      behaviorMessage = "客户端已自动重启";
    } else if (programStarted || (payload?.started === true && payload?.was_running !== true)) {
      behaviorMessage = "客户端已自动启动";
    } else if (programRestartRequired || (payload?.was_running === true && payload?.restart_required === true)) {
      behaviorMessage = "请手动重启客户端程序";
    } else if (payload?.was_running !== true && payload?.started !== true) {
      behaviorMessage = "客户端当前未运行，未自动启动";
    }
    const warning = warnings.length ? `；${warnings.join("；")}` : "";
    const routeMessage = payload?.direct_upstream
      ? `已直连 ${payload.provider || "当前供应商"}`
      : "已接入本地 API";
    showToast(`已一键配置 ${name}${path}；${routeMessage}；${behaviorMessage}${warning}`, warning ? "error" : "success");
    setStatus(`${name} 已配置；${behaviorMessage}`);
  } finally {
    if (button) button.disabled = false;
  }
}

async function updateClientRouteSetting(client, name) {
  clientRouteEnabled[client] = clientRouteInputs[client]?.checked === true;
  const enabled = clientRouteEnabled[client];
  await persistConfig(enabled
    ? `已启用 ${name} 路由`
    : `已关闭 ${name} 路由`);
}

function normalizeClientRoutes(routes) {
  return {
    codex: routes?.codex === true,
    opencode: routes?.opencode === true,
    "claude-code": routes?.["claude-code"] === true,
    openclaw: routes?.openclaw === true
  };
}

function syncClientRouteInputs() {
  Object.entries(clientRouteInputs).forEach(([client, input]) => {
    if (input) input.checked = clientRouteEnabled[client] === true;
  });
}

async function applyEnabledClientRoutes() {
  const res = await fetch("/api/client/routes/apply", {method: "POST"});
  if (!res.ok) throw new Error(await readErrorMessage(res));
  const payload = await res.json();
  const errors = Array.isArray(payload?.errors) ? payload.errors.filter(Boolean) : [];
  if (errors.length) throw new Error(errors.join("；"));
  return Array.isArray(payload?.clients) ? payload.clients : [];
}

async function switchTextProvider(profile) {
  let providerSwitched = false;
  try {
    applyTextProfile(profile.id);
    renderTextProfiles();
    await persistConfig("");
    providerSwitched = true;
    const updatedClients = await applyEnabledClientRoutes();
    const providerName = profile.name || profile.provider || "未命名";
    const directUpstream = programSettings.localAPIEnabled === false;
    if (updatedClients.length === 0) {
      showToast(`已切换供应商：${providerName}；当前未启用客户端路由`, "success");
      setStatus(`已切换供应商：${providerName}`);
      return;
    }
    const clientNames = updatedClients.map((client) => client.name || client.client).join("、");
    showToast(directUpstream
      ? `供应商切换成功，已将 ${clientNames} 更新为直连 ${profile.provider || providerName}；请重启客户端程序`
      : `供应商切换成功，已更新 ${clientNames}；请重启客户端程序`, "success");
    setStatus(`供应商切换成功，请重启 ${clientNames}`);
  } catch (err) {
    err.providerSwitched = providerSwitched;
    throw err;
  }
}

async function restoreCodexOfficialMode() {
  restoreCodex.disabled = true;
  try {
    const res = await fetch("/api/client/restore", {
      method: "POST",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({client: "codex"})
    });
    if (!res.ok) throw new Error(await readErrorMessage(res));
    clientRouteEnabled.codex = false;
    await loadConfig();
    showToast("已恢复 Codex 官方模式并关闭路由；请重新启动 Codex", "success");
    setStatus("Codex 官方模式已恢复");
  } finally {
    restoreCodex.disabled = false;
  }
}

async function updateCodexHistorySetting(enabled) {
  let settingPersisted = false;
  try {
    if (enabled) {
      const migrateExisting = await confirmAction({
        title: "开启统一会话历史",
        message: "系统将安全地整理现有 Codex 会话，并保留一份迁移前备份。",
        variant: "success",
        steps: [
          "自动备份当前官方会话数据",
          "合并官方与第三方历史记录",
          "后续会话统一展示与管理"
        ],
        alertTitle: "请注意",
        alertMessage: "含加密推理内容的旧会话，跨供应商继续时可能无法恢复完整上下文。",
        confirmText: "确认开启",
        cancelText: "暂不迁移"
      });
      await persistConfig("已开启 Codex 统一会话历史");
      settingPersisted = true;
      const prepared = await runCodexHistoryAction("prepare");
      if (prepared.config_updated) {
        showToast("已将当前官方 provider 统一为 custom；请重新启动 Codex", "success");
      }
      if (!migrateExisting) return;
      const result = await runCodexHistoryAction("migrate");
      showToast(`已迁移 ${result.sessions || 0} 个会话、${result.threads || 0} 条线程`, "success");
      return;
    }

    const statusRes = await fetch("/api/client/codex/history");
    if (!statusRes.ok) throw new Error(await readErrorMessage(statusRes));
    const historyStatus = await statusRes.json();
    const restoreExisting = historyStatus.has_backup && await confirmAction({
      title: "恢复原官方会话标识？",
      message: "检测到统一历史迁移备份，可把迁移前的官方会话精确恢复为 openai 标识。",
      variant: "warning",
      alertTitle: "恢复范围",
      alertMessage: "开启统一历史后新建的第三方会话不会被改回。",
      confirmText: "恢复备份",
      cancelText: "仅关闭功能"
    });
    await persistConfig("已关闭 Codex 统一会话历史");
    settingPersisted = true;
    const unprepared = await runCodexHistoryAction("unprepare");
    if (unprepared.config_updated) {
      showToast("已将官方 provider 恢复为 openai；请重新启动 Codex", "success");
    }
    if (!restoreExisting) return;
    const result = await runCodexHistoryAction("restore");
    showToast(`已恢复 ${result.sessions || 0} 个会话、${result.threads || 0} 条线程`, "success");
  } catch (err) {
    err.codexSettingPersisted = settingPersisted;
    throw err;
  }
}

async function runCodexHistoryAction(action) {
  const res = await fetch("/api/client/codex/history", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({action})
  });
  if (!res.ok) throw new Error(await readErrorMessage(res));
  return await res.json();
}

async function loadDashboard() {
  const requestSequence = ++dashboardRequestSequence;
  dashboardRequestController?.abort();
  const controller = new AbortController();
  dashboardRequestController = controller;
  refreshDashboard.disabled = true;
  try {
    const params = new URLSearchParams({period: dashboardPeriod});
    if (dashboardSupplierFilter) params.set("supplier", dashboardSupplierFilter);
    if (dashboardModelFilter) params.set("model", dashboardModelFilter);
    const res = await fetch(`/api/dashboard?${params.toString()}`, {signal: controller.signal});
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const payload = await res.json();
    if (requestSequence !== dashboardRequestSequence) return;
    dashboardPayload = payload;
    renderDashboard(payload);
  } catch (err) {
    if (err?.name === "AbortError") return;
    throw err;
  } finally {
    if (requestSequence === dashboardRequestSequence) {
      refreshDashboard.disabled = false;
      dashboardRequestController = null;
    }
  }
}

function renderDashboard(payload) {
  const summary = payload?.summary || {};
  renderDashboardFilters(payload?.options || {});
  setDashboardToken("dashboardLifetimeTokens", summary.lifetime_tokens);
  setDashboardToken("dashboardTodayTokens", summary.today_tokens);
  setDashboardToken("dashboardPeriodTokens", summary.period_tokens);
  setDashboardToken("dashboardInputTokens", summary.input_tokens);
  setDashboardToken("dashboardOutputTokens", summary.output_tokens);
  setDashboardToken("dashboardCacheTokens", summary.cache_hit_tokens);
  setDashboardText("dashboardRequests", formatNumber(summary.requests));
  setDashboardText("dashboardFailures", formatNumber(summary.failures));
  setDashboardText("dashboardFailureSummary", `失败 ${formatNumber(summary.failures)} 次`);
  setDashboardText("dashboardAverageFirst", formatDashboardDuration(summary.average_first_token_ms));
  setDashboardText("dashboardAverageDuration", formatDashboardDuration(summary.average_duration_ms));
  setDashboardText("dashboardUpdatedAt", `更新于 ${new Date(payload.generated_at).toLocaleString()}`);
  const periodLabels = {day: "当日统计", "7d": "近 7 天统计", month: "本月统计"};
  setDashboardText("dashboardPeriodLabel", periodLabels[payload.period] || "周期统计");
  renderDashboardTokenTrend(payload);
  renderDashboardComposition(summary);
  renderDashboardModels(Array.isArray(payload.models) ? payload.models : []);
  renderDashboardRequests(Array.isArray(payload.series) ? payload.series : []);
}

function renderDashboardFilters(options) {
  fillDashboardSelect(dashboardSupplier, options.suppliers, "全部供应商", dashboardSupplierFilter);
  fillDashboardSelect(dashboardModel, options.models, "全部模型", dashboardModelFilter);
}

function fillDashboardSelect(select, values, allLabel, selectedValue) {
  const fragment = document.createDocumentFragment();
  const allOption = document.createElement("option");
  allOption.value = "";
  allOption.textContent = allLabel;
  fragment.appendChild(allOption);
  (Array.isArray(values) ? values : []).forEach((value) => {
    const option = document.createElement("option");
    option.value = String(value);
    option.textContent = String(value);
    fragment.appendChild(option);
  });
  select.replaceChildren(fragment);
  select.value = selectedValue;
}

function setDashboardText(id, value) {
  const element = document.querySelector(`#${id}`);
  if (element) element.textContent = value;
}

function setDashboardToken(id, value) {
  const element = document.querySelector(`#${id}`);
  if (!element) return;
  element.textContent = formatCompactNumber(value);
  element.title = `${formatNumber(value)} Token`;
}

function formatDashboardDuration(value) {
  const duration = Number(value || 0);
  return duration > 0 ? `${formatNumber(Math.round(duration))} ms` : "-";
}

function renderDashboardTokenTrend(payload) {
  const buckets = Array.isArray(payload.series) ? payload.series : [];
  let chartSeries;
  if (dashboardChartMode === "model") {
    const colors = ["#2563eb", "#8b5cf6", "#0ea5e9", "#f59e0b", "#10b981"];
    chartSeries = (Array.isArray(payload.models) ? payload.models : []).slice(0, 5).map((model, index) => ({
      name: `${model.model} · ${model.supplier}`,
      color: colors[index],
      values: buckets.map((bucket) => Number(bucket.models?.[model.series_key] || 0))
    }));
  } else {
    chartSeries = [
      {name: "输入", color: "#2563eb", values: buckets.map((bucket) => Number(bucket.input_tokens || 0))},
      {name: "输出", color: "#8b5cf6", values: buckets.map((bucket) => Number(bucket.output_tokens || 0))},
      {name: "缓存命中", color: "#10b981", values: buckets.map((bucket) => Number(bucket.cache_hit_tokens || 0))}
    ];
  }
  dashboardTokenLegend.innerHTML = chartSeries.map((series) => `<span><i style="background:${series.color}"></i>${escapeHTML(series.name)}</span>`).join("");
  renderDashboardLineChart(dashboardTokenChart, buckets.map((bucket) => bucket.label), chartSeries);
}
function renderDashboardComposition(summary) {
  const input = Number(summary.input_tokens || 0);
  const output = Number(summary.output_tokens || 0);
  const cache = Number(summary.cache_hit_tokens || 0);
  const uncachedInput = Math.max(0, input - cache);
  const total = Math.max(0, uncachedInput + output + cache);
  const values = [
    {name: "普通输入", value: uncachedInput, color: "#2563eb"},
    {name: "输出", value: output, color: "#8b5cf6"},
    {name: "缓存命中", value: cache, color: "#10b981"}
  ];
  let offset = 0;
  const stops = values.map((item) => {
    const start = total > 0 ? offset / total * 100 : 0;
    offset += item.value;
    const end = total > 0 ? offset / total * 100 : 0;
    return `${item.color} ${start}% ${end}%`;
  });
  const donut = document.querySelector("#dashboardTokenDonut");
  donut.style.background = total > 0 ? `conic-gradient(${stops.join(",")})` : "#e8eef6";
  setDashboardText("dashboardDonutTotal", formatCompactNumber(Number(summary.period_tokens || 0)));
  const list = document.querySelector("#dashboardCompositionList");
  list.innerHTML = values.map((item) => {
    const percent = total > 0 ? item.value / total * 100 : 0;
    return `<div><span><i style="background:${item.color}"></i>${item.name}</span><strong>${formatCompactNumber(item.value)} <small>${percent.toFixed(1)}%</small></strong></div>`;
  }).join("");
}

function renderDashboardModels(models) {
  if (models.length === 0) {
    dashboardModelRows.innerHTML = '<tr><td class="dashboard-table-empty" colspan="7">当前筛选范围内暂无模型用量</td></tr>';
    return;
  }
  dashboardModelRows.innerHTML = models.map((model) => `
    <tr>
      <td><strong>${escapeHTML(model.model || "-")}</strong></td>
      <td>${escapeHTML(model.supplier || "-")}</td>
      <td title="${formatNumber(model.input_tokens)} Token">${formatCompactNumber(model.input_tokens)}</td>
      <td title="${formatNumber(model.output_tokens)} Token">${formatCompactNumber(model.output_tokens)}</td>
      <td title="${formatNumber(model.cache_hit_tokens)} Token">${formatCompactNumber(model.cache_hit_tokens)}</td>
      <td title="${formatNumber(model.total_tokens)} Token"><strong>${formatCompactNumber(model.total_tokens)}</strong></td>
      <td>${formatNumber(model.requests)}</td>
    </tr>
  `).join("");
}

function renderDashboardRequests(buckets) {
  const labels = buckets.map((bucket) => bucket.label);
  const values = buckets.map((bucket) => Number(bucket.requests || 0));
  renderDashboardBarChart(dashboardRequestChart, labels, values);
}

function renderDashboardLineChart(container, labels, seriesList) {
  if (!container) return;
  const width = Math.max(420, Math.round(container.getBoundingClientRect().width || 900));
  const height = 360;
  const padding = {left: 64, right: 22, top: 22, bottom: 44};
  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;
  const values = seriesList.flatMap((series) => series.values);
  const maxValue = Math.max(1, ...values);
  const yMax = dashboardAxisMaximum(maxValue);
  const x = (index) => padding.left + (labels.length <= 1 ? 0 : index * plotWidth / (labels.length - 1));
  const y = (value) => padding.top + plotHeight - Number(value || 0) / yMax * plotHeight;
  const grid = [];
  for (let index = 0; index <= 5; index++) {
    const value = yMax * (5 - index) / 5;
    const position = padding.top + plotHeight * index / 5;
    grid.push(`<line x1="${padding.left}" y1="${position}" x2="${width - padding.right}" y2="${position}" />`);
    grid.push(`<text x="${padding.left - 10}" y="${position + 4}" text-anchor="end">${formatCompactNumber(value)}</text>`);
  }
  const labelStep = Math.max(1, Math.ceil(labels.length / 8));
  const xLabels = labels.map((label, index) => index % labelStep === 0 || index === labels.length - 1
    ? `<text x="${x(index)}" y="${height - 12}" text-anchor="middle">${escapeHTML(label)}</text>` : "").join("");
  const lines = seriesList.map((series) => {
    const points = series.values.map((value, index) => `${x(index)},${y(value)}`).join(" ");
    const dots = series.values.map((value, index) => Number(value || 0) > 0
      ? `<circle class="dashboard-chart-point" cx="${x(index)}" cy="${y(value)}" r="4.5" fill="${series.color}" />` : "").join("");
    return `<polyline points="${points}" fill="none" stroke="${series.color}" stroke-width="2.75" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke" />${dots}`;
  }).join("");
  container.innerHTML = `
    <svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Token usage trend">
      <g class="dashboard-chart-grid">${grid.join("")}${xLabels}</g>
      ${lines}
    </svg>
    <div class="dashboard-chart-crosshair" hidden></div>
    <div class="dashboard-chart-tooltip" role="status" hidden></div>`;

  const svg = container.querySelector("svg");
  const crosshair = container.querySelector(".dashboard-chart-crosshair");
  const tooltip = container.querySelector(".dashboard-chart-tooltip");
  const hideTooltip = () => {
    crosshair.hidden = true;
    tooltip.hidden = true;
  };
  container.onpointerleave = hideTooltip;
  container.onpointermove = (event) => {
    if (labels.length === 0 || seriesList.length === 0) {
      hideTooltip();
      return;
    }
    const svgRect = svg.getBoundingClientRect();
    const containerRect = container.getBoundingClientRect();
    const viewX = (event.clientX - svgRect.left) / Math.max(1, svgRect.width) * width;
    if (viewX < padding.left || viewX > width - padding.right) {
      hideTooltip();
      return;
    }
    const rawIndex = labels.length <= 1 ? 0 : (viewX - padding.left) / plotWidth * (labels.length - 1);
    const index = Math.max(0, Math.min(labels.length - 1, Math.round(rawIndex)));
    const scaleX = svgRect.width / width;
    const scaleY = svgRect.height / height;
    const crosshairLeft = svgRect.left - containerRect.left + x(index) * scaleX;
    crosshair.style.left = `${crosshairLeft}px`;
    crosshair.style.top = `${svgRect.top - containerRect.top + padding.top * scaleY}px`;
    crosshair.style.height = `${plotHeight * scaleY}px`;
    crosshair.hidden = false;

    const rows = seriesList.map((series) => {
      const value = Number(series.values[index] || 0);
      return `<div><span><i style="background:${series.color}"></i>${escapeHTML(series.name)}</span><strong>${formatNumber(value)} Token</strong></div>`;
    }).join("");
    tooltip.innerHTML = `<b>${escapeHTML(labels[index])}</b>${rows}`;
    tooltip.hidden = false;
    const tooltipWidth = tooltip.offsetWidth;
    const tooltipHeight = tooltip.offsetHeight;
    let left = crosshairLeft + 14;
    if (left + tooltipWidth > containerRect.width - 8) left = crosshairLeft - tooltipWidth - 14;
    left = Math.max(8, Math.min(left, containerRect.width - tooltipWidth - 8));
    const pointerTop = event.clientY - containerRect.top;
    let top = pointerTop - tooltipHeight - 14;
    if (top < 8) top = pointerTop + 14;
    top = Math.max(8, Math.min(top, containerRect.height - tooltipHeight - 8));
    tooltip.style.left = `${left}px`;
    tooltip.style.top = `${top}px`;
  };
}
function renderDashboardBarChart(container, labels, values) {
  if (!container) return;
  const width = Math.max(420, Math.round(container.getBoundingClientRect().width || 900));
  const height = 360;
  const padding = {left: 64, right: 22, top: 22, bottom: 44};
  const plotWidth = width - padding.left - padding.right;
  const plotHeight = height - padding.top - padding.bottom;
  const yMax = dashboardAxisMaximum(Math.max(1, ...values));
  const slotWidth = labels.length > 0 ? plotWidth / labels.length : plotWidth;
  const barWidth = Math.max(5, Math.min(32, slotWidth * 0.58));
  const grid = [];
  for (let index = 0; index <= 5; index++) {
    const value = yMax * (5 - index) / 5;
    const position = padding.top + plotHeight * index / 5;
    grid.push(`<line x1="${padding.left}" y1="${position}" x2="${width - padding.right}" y2="${position}" />`);
    grid.push(`<text x="${padding.left - 9}" y="${position + 4}" text-anchor="end">${formatCompactNumber(value)}</text>`);
  }
  const labelStep = Math.max(1, Math.ceil(labels.length / 8));
  const bars = values.map((value, index) => {
    const barHeight = Number(value || 0) / yMax * plotHeight;
    const x = padding.left + index * slotWidth + (slotWidth - barWidth) / 2;
    const y = padding.top + plotHeight - barHeight;
    const label = index % labelStep === 0 || index === labels.length - 1
      ? `<text x="${x + barWidth / 2}" y="${height - 11}" text-anchor="middle">${escapeHTML(labels[index])}</text>` : "";
    return `<rect class="dashboard-request-bar" x="${x}" y="${y}" width="${barWidth}" height="${Math.max(0, barHeight)}" rx="4" fill="#60a5fa" />${label}`;
  }).join("");
  container.innerHTML = `
    <svg viewBox="0 0 ${width} ${height}" role="img" aria-label="请求量趋势">
      <g class="dashboard-chart-grid">${grid.join("")}${bars}</g>
    </svg>
    <div class="dashboard-chart-crosshair" hidden></div>
    <div class="dashboard-chart-tooltip" role="status" hidden></div>`;

  const svg = container.querySelector("svg");
  const crosshair = container.querySelector(".dashboard-chart-crosshair");
  const tooltip = container.querySelector(".dashboard-chart-tooltip");
  const hideTooltip = () => {
    crosshair.hidden = true;
    tooltip.hidden = true;
  };
  container.onpointerleave = hideTooltip;
  container.onpointermove = (event) => {
    if (labels.length === 0) {
      hideTooltip();
      return;
    }
    const svgRect = svg.getBoundingClientRect();
    const containerRect = container.getBoundingClientRect();
    const viewX = (event.clientX - svgRect.left) / Math.max(1, svgRect.width) * width;
    if (viewX < padding.left || viewX > width - padding.right) {
      hideTooltip();
      return;
    }
    const index = Math.max(0, Math.min(labels.length - 1, Math.floor((viewX - padding.left) / slotWidth)));
    const scaleX = svgRect.width / width;
    const scaleY = svgRect.height / height;
    const crosshairLeft = svgRect.left - containerRect.left + (padding.left + (index + 0.5) * slotWidth) * scaleX;
    crosshair.style.left = `${crosshairLeft}px`;
    crosshair.style.top = `${svgRect.top - containerRect.top + padding.top * scaleY}px`;
    crosshair.style.height = `${plotHeight * scaleY}px`;
    crosshair.hidden = false;

    tooltip.innerHTML = `<b>${escapeHTML(labels[index])}</b><div><span><i style="background:#60a5fa"></i>请求数</span><strong>${formatNumber(values[index])} 次</strong></div>`;
    tooltip.hidden = false;
    const tooltipWidth = tooltip.offsetWidth;
    const tooltipHeight = tooltip.offsetHeight;
    let left = crosshairLeft + 14;
    if (left + tooltipWidth > containerRect.width - 8) left = crosshairLeft - tooltipWidth - 14;
    left = Math.max(8, Math.min(left, containerRect.width - tooltipWidth - 8));
    const pointerTop = event.clientY - containerRect.top;
    let top = pointerTop - tooltipHeight - 14;
    if (top < 8) top = pointerTop + 14;
    top = Math.max(8, Math.min(top, containerRect.height - tooltipHeight - 8));
    tooltip.style.left = `${left}px`;
    tooltip.style.top = `${top}px`;
  };
}
function dashboardAxisMaximum(value) {
  const magnitude = 10 ** Math.floor(Math.log10(Math.max(1, value)));
  return Math.ceil(value / magnitude) * magnitude;
}

function formatCompactNumber(value) {
  const number = Number(value || 0);
  const absolute = Math.abs(number);
  if (absolute >= 1000000000) return `${formatScaledNumber(number / 1000000000)}B`;
  if (absolute >= 1000000) return `${formatScaledNumber(number / 1000000)}M`;
  if (absolute >= 1000) return `${formatScaledNumber(number / 1000)}K`;
  return String(Math.round(number));
}

function formatScaledNumber(value) {
  const absolute = Math.abs(value);
  const digits = absolute >= 100 ? 0 : (absolute >= 10 ? 1 : 2);
  const fixed = value.toFixed(digits);
  return digits === 0 ? fixed : fixed.replace(/\.?0+$/, "");
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
    const status = Number(log.status || 0);
    const failed = status >= 400 || Boolean(log.error);
    const usageAvailable = hasTokenUsage(log);
    const statusText = status > 0 ? `${status} ${failed ? "失败" : "成功"}` : "状态未知";
    item.className = `log-item ${failed ? "failed" : "succeeded"}`;
    item.innerHTML = `
      <div class="log-head">
        <div class="log-identity">
          <strong class="log-model">${escapeHTML(log.model || "未知模型")}</strong>
          <div class="log-meta">
            <span class="log-supplier">供应商 ${escapeHTML(formatUpstream(log))}</span>
            <span class="log-mode ${formatRequestMode(log).className}">${formatRequestMode(log).label}</span>
            <span class="log-meta-separator" aria-hidden="true">·</span>
            <span>${escapeHTML(new Date(log.at).toLocaleString())}</span>
            <span class="log-meta-separator" aria-hidden="true">·</span>
            <span>${escapeHTML(log.method || "")} ${escapeHTML(log.path || "")}</span>
          </div>
        </div>
        <em class="${failed ? "log-status error" : "log-status"}">${escapeHTML(statusText)}</em>
      </div>
      <div class="log-token-grid">
        ${renderLogMetric("输入", formatTokenUsage(log.input_tokens, usageAvailable))}
        ${renderLogMetric("输出", formatTokenUsage(log.output_tokens, usageAvailable))}
        ${renderLogMetric("总计", formatTokenUsage(log.total_tokens, usageAvailable))}
        ${renderLogMetric("缓存命中", formatTokenUsage(log.cache_hit_tokens, usageAvailable))}
        ${renderLogMetric("首 Token", formatFirstTokenDuration(log.first_token_ms))}
      </div>
      ${log.error ? `<div class="log-error"><span class="log-error-icon" aria-hidden="true">!</span><span>${escapeHTML(log.error)}</span></div>` : ""}
      <div class="log-duration" title="总耗时"><span class="log-duration-icon" aria-hidden="true"></span><span class="log-duration-label">总耗时</span><strong>${formatLogDuration(log.duration_ms)}</strong></div>
    `;
    logList.appendChild(item);
  });
}

function renderLogMetric(label, value) {
  return `<div><span>${label}</span><strong>${value}</strong></div>`;
}
function hasTokenUsage(log) {
  return [log.input_tokens, log.output_tokens, log.total_tokens].some((value) => Number(value || 0) > 0);
}

function formatTokenUsage(value, available) {
  return available ? formatCompactNumber(value) : "-";
}

function formatRequestMode(log) {
  switch (String(log.request_mode || "").toLowerCase()) {
    case "stream":
      return {className: "stream", label: "流式"};
    case "sync":
      return {className: "sync", label: "同步"};
    default:
      return {className: "unknown", label: "未知"};
  }
}

function formatUpstream(log) {
  const name = String(log.upstream_name || "").trim();
  const provider = String(log.upstream_provider || "").trim();
  return name || provider || "-";
}

function formatFirstTokenDuration(value) {
  const duration = Number(value || 0);
  return duration > 0 ? `${(duration / 1000).toFixed(1)}s` : "-";
}

function formatLogDuration(value) {
  const duration = Number(value || 0);
  if (duration <= 0) return "-";
  if (duration < 1000) return `${formatNumber(duration)} ms`;
  return `${(duration / 1000).toFixed(1)}s`;
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
  if (homeBaseURL) homeBaseURL.textContent = location.host;
  if (homeTextModel) homeTextModel.textContent = formatTextModelList(textProfile, "使用请求模型名");
  if (homeTextProvider) homeTextProvider.textContent = profileHeadline(textProfile, "text");
  if (homeVisionModel) homeVisionModel.textContent = visionProfile?.model || "未设置模型";
  if (homeVisionProvider) homeVisionProvider.textContent = profileHeadline(visionProfile, "vision");
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
    row.dataset.profileId = profile.id;
    row.innerHTML = `
      <span class="profile-drag-handle" role="button" tabindex="0" aria-label="拖动 ${escapeHTML(profile.name || "未命名")} 排序" title="按住拖动排序">
        <span aria-hidden="true"></span>
      </span>
      <div class="profile-main">
        <div>
          <strong>${escapeHTML(profile.name || "未命名")}</strong>
          <span>${escapeHTML(profileSummary(profile, kind))}</span>
        </div>
      </div>
      <div class="profile-actions">
        <button class="secondary small-action profile-switch" type="button" data-action="switch"${profile.id === activeId ? " disabled" : ""}>使用</button>
        <button class="secondary small-action" type="button" data-action="edit">编辑</button>
        <button class="danger small-action" type="button" data-action="delete">删除</button>
      </div>
    `;
    row.querySelector('[data-action="switch"]').addEventListener("click", (event) => {
      if (profile.id === activeId) return;
      event.currentTarget.disabled = true;
      if (kind === "text") {
        switchTextProvider(profile).catch((err) => {
          console.error(err);
          const prefix = err.providerSwitched
            ? "供应商已切换，但客户端路由更新失败"
            : "切换供应商失败";
          showToast(`${prefix}：${err.message || err}`, "error");
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
    const dragHandle = row.querySelector(".profile-drag-handle");
    dragHandle.addEventListener("mousedown", (event) => {
      if (event.button !== 0) return;
      event.preventDefault();
      profileDragState = {kind, id: profile.id, targetId: "", insertAfter: false, container, row};
      row.classList.add("dragging");
    });
    container.appendChild(row);
  });
}

function setProfileDropIndicator(container, targetRow, insertAfter) {
  container.querySelectorAll(".profile-row").forEach((row) => {
    row.classList.remove("drop-before", "drop-after");
  });
  targetRow.classList.add(insertAfter ? "drop-after" : "drop-before");
}

function clearProfileDragState(container) {
  profileDragState = null;
  container.querySelectorAll(".profile-row").forEach((row) => {
    row.classList.remove("dragging", "drop-before", "drop-after");
  });
}

function updateProfileDrag(event) {
  if (!profileDragState) return;
  event.preventDefault();
  const {container, id} = profileDragState;
  const targetRow = document.elementFromPoint(event.clientX, event.clientY)?.closest(".profile-row");
  if (!targetRow || !container.contains(targetRow) || targetRow.dataset.profileId === id) {
    profileDragState.targetId = "";
    container.querySelectorAll(".profile-row").forEach((item) => item.classList.remove("drop-before", "drop-after"));
    return;
  }
  const targetRect = targetRow.getBoundingClientRect();
  const insertAfter = event.clientY >= targetRect.top + targetRect.height / 2;
  profileDragState.targetId = targetRow.dataset.profileId;
  profileDragState.insertAfter = insertAfter;
  setProfileDropIndicator(container, targetRow, insertAfter);
}

function finishProfileDrag() {
  if (!profileDragState) return;
  const {kind, id: draggedId, targetId, insertAfter, container} = profileDragState;
  clearProfileDragState(container);
  if (targetId) reorderProfiles(kind, draggedId, targetId, insertAfter);
}

document.addEventListener("mousemove", updateProfileDrag);
document.addEventListener("mouseup", finishProfileDrag);

function reorderProfiles(kind, draggedId, targetId, insertAfter) {
  const profiles = kind === "text" ? textProfiles : visionProfiles;
  const sourceIndex = profiles.findIndex((profile) => profile.id === draggedId);
  if (sourceIndex < 0 || draggedId === targetId) return;
  const previousOrder = profiles.map((profile) => profile.id).join("\n");
  const [draggedProfile] = profiles.splice(sourceIndex, 1);
  const targetIndex = profiles.findIndex((profile) => profile.id === targetId);
  if (targetIndex < 0) {
    profiles.splice(sourceIndex, 0, draggedProfile);
    return;
  }
  profiles.splice(targetIndex + (insertAfter ? 1 : 0), 0, draggedProfile);
  if (profiles.map((profile) => profile.id).join("\n") === previousOrder) return;
  if (kind === "text") {
    renderTextProfiles();
  } else {
    renderVisionProfiles();
  }
  persistConfig(kind === "text" ? "文本模型顺序已保存" : "视觉模型顺序已保存").catch((err) => {
    console.error(err);
    showToast(`排序保存失败：${err.message || err}`, "error");
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
  modalProfileModelLabel.textContent = "模型名";
  modalProfileModel.placeholder = isText ? "可填多个，换行或逗号分隔；留空则使用客户端请求里的 model" : "例如 gpt-4o-mini";
  modalProfileModel.value = isText ? textProfileModels(profile).join("\n") : profile?.model || "";
  modalModelMappings = isText ? textProfileMappings(profile) : [];
  modalProfileModelWrap.hidden = isText;
  modelMappingSection.hidden = !isText;
  fetchModels.hidden = isText;
  renderModelMappingRows();
  modalProfileProxyWrap.hidden = !isText;
  modalProfileWireAPIWrap.hidden = !isText;
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

const reasoningEffortChoices = [
  {value: "none", label: "\u4e0d\u652f\u6301"},
  {value: "low", label: "\u4f4e"},
  {value: "medium", label: "\u4e2d"},
  {value: "high", label: "\u9ad8"},
  {value: "xhigh", label: "\u8d85\u9ad8"}
];

function reasoningEffortOptions(selected) {
  return reasoningEffortChoices
    .map((choice) => `<option value="${choice.value}" ${selected === choice.value ? "selected" : ""}>${choice.label}</option>`)
    .join("");
}

function renderModelMappingRows() {
  if (!modelMappingRows) return;
  modelMappingRows.innerHTML = "";
  if (modalModelMappings.length === 0) return;
  modalModelMappings.forEach((mapping, index) => {
    const row = document.createElement("div");
    row.className = "model-mapping-row";
    row.innerHTML = `
      <input data-field="name" value="${escapeAttr(mapping.name || "")}" placeholder="例如：DeepSeek V4 Flash">
      <input data-field="model" value="${escapeAttr(mapping.model || "")}" placeholder="例如：deepseek-v4-flash">
      <input data-field="context_window" type="number" min="0" step="1" value="${escapeAttr(mapping.context_window || "")}" placeholder="例如：128000">
      <label class="model-mapping-supports-images" title="勾选后图片直接发送给该文本模型">
        <input data-field="supports_images" type="checkbox" ${mapping.supports_images === true ? "checked" : ""}>
        <span>多模态</span>
      </label>
      <label class="model-mapping-supports-reasoning" title="\u9009\u62e9\u8be5\u6a21\u578b\u652f\u6301\u7684\u63a8\u7406\u5f3a\u5ea6">
        <select data-field="reasoning_effort" aria-label="\u63a8\u7406\u5f3a\u5ea6">
          ${reasoningEffortOptions(mapping.reasoning_effort)}
        </select>
      </label>
      <button class="icon-button model-mapping-delete" type="button" aria-label="删除模型">×</button>
    `;
    row.querySelectorAll("input, select").forEach((input) => {
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
    const rows = modelMappingRows.querySelectorAll(".model-mapping-row");
    rows[rows.length - 1]?.querySelector('[data-field="name"]')?.focus();
  }
}

function updateModelMappingFromRows() {
  if (!modelMappingRows) return;
  modalModelMappings = [...modelMappingRows.querySelectorAll(".model-mapping-row")]
    .map((row) => normalizeModelMapping({
      name: row.querySelector('[data-field="name"]')?.value,
      model: row.querySelector('[data-field="model"]')?.value,
      context_window: row.querySelector('[data-field="context_window"]')?.value,
      supports_images: row.querySelector('[data-field="supports_images"]')?.checked === true,
      reasoning_effort: row.querySelector('[data-field="reasoning_effort"]')?.value
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
        modalModelMappings.push(normalizeModelMapping({name: model, model}));
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
  const mapping = value && typeof value === "object" && !Array.isArray(value) ? value : {};
  const scalar = typeof value === "string" || typeof value === "number" ? String(value).trim() : "";
  const name = String(mapping.name || mapping.display_name || scalar).trim();
  const model = String(mapping.model || scalar).trim();
  const contextWindow = Number(value?.context_window || value?.contextWindow || 0);
  const configuredReasoningEffort = normalizeReasoningEffort(mapping.reasoning_effort);
  const reasoningEffort = configuredReasoningEffort || (typeof mapping.supports_reasoning === "boolean"
    ? (mapping.supports_reasoning ? "high" : "none")
    : (inferModelReasoningSupport(name, model) ? "high" : "none"));
  return {
    name: name || model,
    model: model || name,
    context_window: Number.isFinite(contextWindow) && contextWindow > 0 ? Math.floor(contextWindow) : 0,
    supports_images: value?.supports_images === true,
    reasoning_effort: reasoningEffort
  };
}

function normalizeReasoningEffort(value) {
  const effort = String(value || "").trim().toLowerCase();
  if (["none", "low", "medium", "high", "xhigh"].includes(effort)) return effort;
  if (["extra-high", "extra_high"].includes(effort)) return "xhigh";
  return "";
}

function reasoningEffortDescription(effort) {
  return {
    low: "Low reasoning",
    medium: "Medium reasoning",
    high: "High reasoning",
    xhigh: "Extra high reasoning"
  }[effort] || "Enable reasoning";
}

function inferModelReasoningSupport(...values) {
  const value = values.join(" ").toLowerCase();
  const markers = [
    "reasoning", "reasoner", "thinking",
    "deepseek-r1", "deepseek-v4",
    "glm-4.5", "glm-4.6", "glm-4.7", "glm-5",
    "grok-3-mini", "grok-4",
    "gpt-5", "qwen3", "gemini-2.5", "gemini-3"
  ];
  if (markers.some((marker) => value.includes(marker))) return true;
  return value.split(/[\/_.:\s-]+/).some((token) => token === "o1" || token === "o3" || token === "o4");
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

function visionInputModalities(imageEnabled) {
  return imageEnabled ? ["text", "image"] : ["text"];
}

function clientImageInputEnabled(mapping) {
  if (programSettings.localAPIEnabled === false) {
    return mapping?.supports_images === true;
  }
  return mapping?.supports_images === true || visionCapabilityEnabled;
}

function clientVersionedBaseURL(profile) {
  const baseURL = String(profile?.base_url || defaultBaseURL(profile?.provider)).trim().replace(/\/+$/, "");
  if (/\/(?:v1|v1beta)$/i.test(baseURL)) return baseURL;
  return `${baseURL}/${String(profile?.provider || "").toLowerCase() === "gemini" ? "v1beta" : "v1"}`;
}

function claudeDesktopGatewayBaseURL(profile) {
  const baseURL = String(profile?.base_url || defaultBaseURL(profile?.provider)).trim().replace(/\/+$/, "");
  return baseURL.replace(/\/(?:v1|v1beta)$/i, "");
}

function directClientMappings(mappings) {
  const seen = new Set();
  return mappings.map((mapping) => {
    const model = String(mapping.model || mapping.name || "").trim();
    return {...mapping, name: model, model};
  }).filter((mapping) => mapping.model && !seen.has(mapping.model) && seen.add(mapping.model));
}

function normalizedDirectProvider(profile) {
  const provider = String(profile?.provider || "").trim().toLowerCase();
  if (!provider || ["openai-compatible", "openai_compatible"].includes(provider)) return "openai";
  if (provider === "claude") return "anthropic";
  if (provider === "google") return "gemini";
  return provider;
}

function directClientCompatibilityMessage(client, profile) {
  const provider = normalizedDirectProvider(profile);
  if (client === "codex" && (provider !== "openai" || normalizeWireAPI(profile?.wire_api) !== "responses")) {
    return "关闭本地 API 后，Codex 仅支持直连使用 Responses 协议的 OpenAI 兼容供应商。";
  }
  if (client === "claude-code" && provider !== "anthropic") {
    return "关闭本地 API 后，Claude 仅支持直连 Anthropic 协议供应商。";
  }
  return "";
}

function openCodeProviderNPM(profile, directUpstream) {
  if (!directUpstream) return "@ai-sdk/openai-compatible";
  switch (String(profile?.provider || "").toLowerCase()) {
    case "anthropic": return "@ai-sdk/anthropic";
    case "gemini": return "@ai-sdk/google";
    default: return "@ai-sdk/openai-compatible";
  }
}

function openClawDirectAPI(profile) {
  switch (String(profile?.provider || "").toLowerCase()) {
    case "anthropic": return "anthropic-messages";
    case "gemini": return "google-generative-ai";
    default: return normalizeWireAPI(profile?.wire_api) === "responses" ? "openai-responses" : "openai-completions";
  }
}

function openClawDirectBaseURL(profile) {
  const provider = String(profile?.provider || "").toLowerCase();
  if (["anthropic", "gemini"].includes(provider)) {
    return String(profile?.base_url || defaultBaseURL(provider)).trim().replace(/\/+$/, "");
  }
  return clientVersionedBaseURL(profile);
}

function renderOpenCodeSnippet() {
  const profile = activeTextProfile();
  const directUpstream = programSettings.localAPIEnabled === false;
  const sourceMappings = textProfileMappings(profile);
  const mappings = directUpstream ? directClientMappings(sourceMappings) : sourceMappings;
  const providerDisplayName = directUpstream ? `${profile?.provider || "供应商"}（直连）` : "Vision Relay";
  const upstreamKey = directUpstream ? String(profile?.api_key || "").trim() : "";
  const preserveOfficialAuth = preserveCodexOfficialAuth?.checked !== false;
  const codexRequiresOpenAIAuth = directUpstream || preserveOfficialAuth;
  const codexBearerToken = preserveOfficialAuth ? (directUpstream ? upstreamKey : "vision-relay-local") : "";
  if (directUpstream && mappings.length === 0) {
    const message = "关闭本地 API 后，请先为当前文本供应商添加至少一个模型。";
    [opencodeConfig, openclawConfig, codexConfig, claudeCodeConfig].forEach((element) => {
      if (element) element.textContent = message;
    });
    return;
  }
  const snippetMappings = mappings.length ? mappings : [normalizeModelMapping({name: "z-ai/glm-5.2", model: "z-ai/glm-5.2"})];
  const defaultClientModel = (snippetMappings[0].name || snippetMappings[0].model || "z-ai/glm-5.2").trim();
  if (opencodeConfig) {
    opencodeConfig.textContent = JSON.stringify({
      "$schema": "https://opencode.ai/config.json",
      provider: {
        "vision-relay": {
          npm: openCodeProviderNPM(profile, directUpstream),
          name: providerDisplayName,
          options: {
            baseURL: directUpstream ? clientVersionedBaseURL(profile) : `${location.origin}/v1`,
            ...(directUpstream ? {apiKey: upstreamKey} : {})
          },
          models: Object.fromEntries(snippetMappings.map((mapping) => {
            const modelName = mapping.name || mapping.model;
            const imageEnabled = clientImageInputEnabled(mapping);
            const inputModalities = visionInputModalities(imageEnabled);
            return [modelName, {
              name: modelName,
              reasoning: mapping.reasoning_effort !== "none",
              attachment: imageEnabled,
              attachments: imageEnabled,
              vision: imageEnabled,
              input_modalities: inputModalities,
              output_modalities: ["text"],
              modalities: {
                input: inputModalities,
                output: ["text"]
              },
              ...(mapping.context_window ? {limit: {context: mapping.context_window}} : {})
            }];
          }))
        }
      },
      model: `vision-relay/${defaultClientModel}`
    }, null, 2);
  }
  if (openclawConfig) {
    const openclawModels = snippetMappings.map((mapping) => {
      const modelName = mapping.name || mapping.model;
      return {
        id: modelName,
        name: modelName,
        input: visionInputModalities(clientImageInputEnabled(mapping)),
        cost: {input: 0, output: 0, cacheRead: 0, cacheWrite: 0},
        contextWindow: mapping.context_window || 128000,
        maxTokens: 8192
      };
    });
    openclawConfig.textContent = JSON.stringify({
      agents: {
        defaults: {
          model: {primary: `vision-relay/${openclawModels[0].id}`}
        }
      },
      models: {
        mode: "merge",
        providers: {
          "vision-relay": {
            baseUrl: directUpstream ? openClawDirectBaseURL(profile) : `${location.origin}/v1`,
            ...(directUpstream ? {apiKey: upstreamKey} : {}),
            api: directUpstream ? openClawDirectAPI(profile) : "openai-completions",
            models: openclawModels
          }
        }
      }
    }, null, 2);
  }
  const codexDirectError = directUpstream ? directClientCompatibilityMessage("codex", profile) : "";
  if (codexConfig && !codexDirectError) {
    const catalogMappings = snippetMappings;
    const codexDefaultModel = catalogMappings[0].name || catalogMappings[0].model;
    const codexDefaultReasoningEffort = catalogMappings[0].reasoning_effort;
    const codexDefaultSupportsReasoning = codexDefaultReasoningEffort !== "none";
    const codexCatalog = {
      models: catalogMappings.map((mapping, index) => {
        const slug = mapping.name || mapping.model;
        const imageEnabled = clientImageInputEnabled(mapping);
        const inputModalities = visionInputModalities(imageEnabled);
        const reasoningEffort = mapping.reasoning_effort;
        const supportsReasoning = reasoningEffort !== "none";
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
        ...(supportsReasoning ? {default_reasoning_level: reasoningEffort} : {}),
        default_reasoning_summary: "none",
        supported_reasoning_levels: supportsReasoning ? [
          {effort: "none", description: "Disable reasoning"},
          {effort: reasoningEffort, description: reasoningEffortDescription(reasoningEffort)}
        ] : [],
        visibility: "list",
        supported_in_api: true,
        priority: 1000 + index,
        shell_type: "shell_command",
        input_modalities: inputModalities,
        supports_parallel_tool_calls: false,
        supports_image_detail_original: imageEnabled,
        supports_reasoning_summaries: supportsReasoning,
        supports_reasoning_summary_parameter: supportsReasoning,
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
      ...(codexDefaultSupportsReasoning ? [`model_reasoning_effort = "${codexDefaultReasoningEffort}"`] : []),
      `model_catalog_json = "vision-relay-model.json"`,
      `web_search = "disabled"`,
      ``,
      `[model_providers.custom]`,
      `name = "${providerDisplayName}"`,
      `wire_api = "responses"`,
      `requires_openai_auth = ${codexRequiresOpenAIAuth}`,
      `base_url = "${directUpstream ? clientVersionedBaseURL(profile) : `${location.origin}/v1`}"`,
      ...(codexBearerToken ? [`experimental_bearer_token = "${codexBearerToken}"`] : []),
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
      ...(codexDefaultSupportsReasoning ? [`model_reasoning_effort = "${codexDefaultReasoningEffort}"`] : []),
      `model_catalog_json = "vision-relay-model.json"`,
      `web_search = "disabled"`,
      ``,
      `[model_providers.custom]`,
      `name = "${providerDisplayName}"`,
      `wire_api = "responses"`,
      `requires_openai_auth = ${codexRequiresOpenAIAuth}`,
      `base_url = "${directUpstream ? clientVersionedBaseURL(profile) : `${location.origin}/v1`}"`,
      ...(codexBearerToken ? [`experimental_bearer_token = "${codexBearerToken}"`] : []),
      ``,
      `[windows]`,
      `sandbox = "unelevated"`
    ].join("\n");
  } else if (codexConfig) {
    codexConfig.textContent = `# ${codexDirectError}`;
  }
  const claudeDirectError = directUpstream ? directClientCompatibilityMessage("claude-code", profile) : "";
  if (claudeCodeConfig && !claudeDirectError) {
    const claudeModels = snippetMappings.map((mapping) => ({
      name: mapping.name || mapping.model,
      ...(Number(mapping.context_window || 0) >= 1000000 ? {supports1m: true} : {})
    }));
    const directAnthropic = directUpstream && normalizedDirectProvider(profile) === "anthropic";
    const claudeDesktopConfig = {
      inferenceProvider: "gateway",
      inferenceGatewayBaseUrl: directUpstream ? claudeDesktopGatewayBaseURL(profile) : location.origin,
      inferenceGatewayAuthScheme: directAnthropic ? "x-api-key" : "bearer",
      inferenceGatewayApiKey: directUpstream ? upstreamKey : "vision-relay",
      inferenceModels: claudeModels,
      disableDeploymentModeChooser: true
    };
    claudeCodeConfig.textContent = JSON.stringify(claudeDesktopConfig, null, 2);
  } else if (claudeCodeConfig) {
    claudeCodeConfig.textContent = claudeDirectError;
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
  let modelMappings = normalizeModelMappings(profile.model_mappings || profile.text_model_mappings || profile.model_overrides || profile.text_model_overrides || profile.model_override);
  if (profile.supports_images === true || profile.text_supports_images === true) {
    modelMappings = modelMappings.map((mapping) => ({...mapping, supports_images: true}));
  }
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

function formatBytes(value) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / Math.pow(1024, index)).toFixed(index ? 1 : 0)} ${units[index]}`;
}


function updateText(html) {
  const node = document.createElement("textarea");
  node.innerHTML = html;
  return node.value;
}

async function checkForUpdate(showErrors = false) {
  checkUpdateButton.disabled = true;
  updateState.textContent = updateText("&#27491;&#22312;&#36830;&#25509; GitHub &#26816;&#26597;&#26356;&#26032;&hellip;");
  try {
    const res = await fetch("/api/update", {cache: "no-store"});
    if (!res.ok) throw new Error(await readErrorMessage(res));
    const info = await res.json();
    currentVersionEl.textContent = info.current_version || "dev";
    latestVersionEl.textContent = info.latest_version || "-";
    updatePublishedAt.textContent = info.published_at ? new Date(info.published_at).toLocaleString() : "-";
    updateNotes.textContent = info.release_notes?.trim() || updateText("&#26412;&#27425; Release &#26242;&#26080;&#21457;&#34892;&#35828;&#26126;&#12290;");
    if (info.release_url) releaseLink.href = info.release_url;
    installUpdateButton.disabled = !info.update_available || !info.can_update;
    if (info.update_available && info.can_update) {
      const size = info.asset_size ? `${updateText("&#65292;&#23433;&#35013;&#21253;")} ${formatBytes(info.asset_size)}` : "";
      updateState.textContent = `${updateText("&#21457;&#29616;&#26032;&#29256;&#26412;")} ${info.latest_version}${size}${updateText("&#65292;&#21487;&#20197;&#19968;&#38190;&#26356;&#26032;&#12290;")}`;
      showToast(`${updateText("&#21457;&#29616;&#26032;&#29256;&#26412;")} ${info.latest_version}`, "success");
    } else if (info.update_available) {
      updateState.textContent = `${updateText("&#21457;&#29616;&#26032;&#29256;&#26412;")} ${info.latest_version}${updateText("&#65292;&#24403;&#21069;&#36816;&#34892;&#26041;&#24335;&#19981;&#25903;&#25345;&#33258;&#21160;&#26367;&#25442;&#65292;&#35831;&#20174; GitHub &#25163;&#21160;&#19979;&#36733;&#12290;")}`;
    } else {
      updateState.textContent = updateText("&#24403;&#21069;&#24050;&#26159;&#26368;&#26032;&#29256;&#26412;&#12290;");
    }
  } catch (err) {
    updateState.textContent = `${updateText("&#26816;&#26597;&#26356;&#26032;&#22833;&#36133;&#65306;")}${err.message || err}`;
    if (showErrors) showToast(updateState.textContent, "error");
  } finally {
    checkUpdateButton.disabled = false;
  }
}

checkUpdateButton.addEventListener("click", () => checkForUpdate(true));
installUpdateButton.addEventListener("click", async () => {
  const confirmed = await confirmAction({
    title: "下载并安装更新？",
    message: "Vision Relay 将下载最新版本，完成校验后关闭当前程序并自动重启。",
    variant: "warning",
    alertTitle: "更新期间请勿关闭程序",
    alertMessage: "安装完成前请保持网络连接。",
    confirmText: "下载并重启",
    cancelText: "稍后更新"
  });
  if (!confirmed) return;
  installUpdateButton.disabled = true;
  checkUpdateButton.disabled = true;
  updateState.textContent = updateText("&#27491;&#22312;&#19979;&#36733;&#24182;&#26657;&#39564;&#26356;&#26032;&#65292;&#35831;&#21247;&#20851;&#38381;&#31243;&#24207;&hellip;");
  try {
    const res = await fetch("/api/update", {method: "POST"});
    if (!res.ok) throw new Error(await readErrorMessage(res));
    const result = await res.json();
    updateState.textContent = result.message || updateText("&#26356;&#26032;&#24050;&#19979;&#36733;&#65292;&#31243;&#24207;&#21363;&#23558;&#37325;&#21551;&hellip;");
    showToast(updateText("&#26356;&#26032;&#24050;&#19979;&#36733;&#65292;&#21363;&#23558;&#37325;&#21551;"), "success");
  } catch (err) {
    updateState.textContent = `${updateText("&#26356;&#26032;&#22833;&#36133;&#65306;")}${err.message || err}`;
    showToast(updateState.textContent, "error");
    installUpdateButton.disabled = false;
    checkUpdateButton.disabled = false;
  }
});

loadConfig().then(() => checkForUpdate(false)).catch((err) => {
  console.error(err);
  setStatus("加载失败");
  setServiceOnline(false);
});
