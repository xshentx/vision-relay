(() => {
  const {createApp, reactive} = Vue;
  const selectBindings = new WeakMap();
  const emptySelectValue = "__vision_relay_empty_option__";

  function componentSelectValue(value) {
    return value === "" ? emptySelectValue : value;
  }

  function nativeSelectValue(value) {
    return value === emptySelectValue ? "" : value;
  }

  function readSelectOptions(select) {
    return [...select.options].map((option, index) => ({
      key: `${option.value}\u0000${index}`,
      value: componentSelectValue(option.value),
      label: option.textContent || option.label || option.value,
      disabled: option.disabled
    }));
  }

  function readSelectValue(select) {
    if (select.multiple) {
      return [...select.selectedOptions].map((option) => componentSelectValue(option.value));
    }
    return componentSelectValue(select.value);
  }

  function enhanceSelect(select) {
    if (!(select instanceof HTMLSelectElement) || selectBindings.has(select)) return;

    const originalAriaHidden = select.getAttribute("aria-hidden");
    const originalTabIndex = select.getAttribute("tabindex");
    const mount = document.createElement("span");
    mount.className = "vr-component-select";
    if (select.dataset.selectCompact === "true" || select.closest(".model-mapping-row")) {
      mount.classList.add("compact");
    }
    if (select.multiple) mount.classList.add("multiple");
    select.insertAdjacentElement("afterend", mount);
    select.classList.add("vr-native-select");
    select.setAttribute("aria-hidden", "true");
    select.tabIndex = -1;

    const state = reactive({
      value: readSelectValue(select),
      options: readSelectOptions(select),
      disabled: select.disabled,
      multiple: select.multiple,
      filterable: select.multiple || select.dataset.selectSearch === "true",
      placeholder: select.dataset.selectPlaceholder || "请选择",
      appendTo: select.closest("dialog") || document.body
    });

    const sync = () => {
      state.options = readSelectOptions(select);
      state.value = readSelectValue(select);
      state.disabled = select.disabled;
      state.multiple = select.multiple;
      state.filterable = select.multiple || select.dataset.selectSearch === "true";
    };

    const commit = (value) => {
      if (select.multiple) {
        const values = new Set(Array.isArray(value) ? value.map((item) => nativeSelectValue(String(item))) : []);
        [...select.options].forEach((option) => {
          option.selected = values.has(option.value);
        });
      } else {
        select.value = value == null ? "" : nativeSelectValue(String(value));
      }
      select.dispatchEvent(new Event("input", {bubbles: true}));
      select.dispatchEvent(new Event("change", {bubbles: true}));
    };

    const selectApp = createApp({
      setup() {
        return {state, commit, sync};
      },
      template: `
        <el-select
          v-model="state.value"
          class="vr-el-select"
          popper-class="vr-component-select-popper"
          :multiple="state.multiple"
          :filterable="state.filterable"
          :disabled="state.disabled"
          :placeholder="state.placeholder"
          :append-to="state.appendTo"
          :collapse-tags="false"
          @visible-change="sync"
          @change="commit"
        >
          <el-option
            v-for="option in state.options"
            :key="option.key"
            :label="option.label"
            :value="option.value"
            :disabled="option.disabled"
          />
        </el-select>
      `
    });
    selectApp.use(ElementPlus);
    selectApp.mount(mount);

    let queued = false;
    const queueSync = () => {
      if (queued) return;
      queued = true;
      queueMicrotask(() => {
        queued = false;
        sync();
      });
    };
    const observer = new MutationObserver(queueSync);
    observer.observe(select, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["disabled", "label", "selected", "value"]
    });
    select.addEventListener("change", sync);
    selectBindings.set(select, {
      sync,
      observer,
      selectApp,
      mount,
      originalAriaHidden,
      originalTabIndex
    });
  }

  function enhanceSelects(root = document) {
    if (root instanceof HTMLSelectElement) enhanceSelect(root);
    root.querySelectorAll?.("select").forEach(enhanceSelect);
  }

  function destroySelect(select) {
    const binding = selectBindings.get(select);
    if (!binding) return;

    binding.observer.disconnect();
    select.removeEventListener("change", binding.sync);
    binding.selectApp.unmount();
    binding.mount.remove();
    select.classList.remove("vr-native-select");
    if (binding.originalAriaHidden == null) {
      select.removeAttribute("aria-hidden");
    } else {
      select.setAttribute("aria-hidden", binding.originalAriaHidden);
    }
    if (binding.originalTabIndex == null) {
      select.removeAttribute("tabindex");
    } else {
      select.setAttribute("tabindex", binding.originalTabIndex);
    }
    selectBindings.delete(select);
  }

  function destroySelects(root) {
    if (root instanceof HTMLSelectElement) destroySelect(root);
    root.querySelectorAll?.("select").forEach(destroySelect);
  }

  function syncSelect(select) {
    selectBindings.get(select)?.sync();
  }

  const state = reactive({
    visible: false,
    title: "请确认",
    message: "",
    variant: "info",
    alertTitle: "",
    alertMessage: "",
    confirmText: "确认",
    cancelText: "取消",
    steps: [],
    requireAcknowledge: false,
    acknowledgeText: "我已了解相关风险",
    acknowledged: false
  });

  let pendingResolve = null;

  function settle(result) {
    const resolve = pendingResolve;
    pendingResolve = null;
    state.visible = false;
    if (resolve) resolve(result);
  }

  const app = createApp({
    setup() {
      const confirmDialog = (options = {}) => {
        if (pendingResolve) pendingResolve(false);
        state.title = options.title || "请确认";
        state.message = options.message || "";
        state.variant = options.variant || "info";
        state.alertTitle = options.alertTitle || "";
        state.alertMessage = options.alertMessage || "";
        state.confirmText = options.confirmText || "确认";
        state.cancelText = options.cancelText || "取消";
        state.steps = Array.isArray(options.steps) ? options.steps : [];
        state.requireAcknowledge = options.requireAcknowledge === true;
        state.acknowledgeText = options.acknowledgeText || "我已了解相关风险";
        state.acknowledged = false;
        state.visible = true;
        return new Promise((resolve) => {
          pendingResolve = resolve;
        });
      };

      const accept = () => {
        if (state.requireAcknowledge && !state.acknowledged) return;
        settle(true);
      };

      const cancel = () => settle(false);

      window.VisionRelayUI = {
        confirm: confirmDialog,
        enhanceSelects,
        syncSelect,
        notify(message, type = "info") {
          ElementPlus.ElMessage({
            message: String(message || ""),
            type: ["success", "warning", "error", "info"].includes(type) ? type : "info",
            duration: 3200,
            showClose: true,
            grouping: true
          });
        }
      };

      return {state, accept, cancel};
    },
    template: `
      <el-dialog
        v-model="state.visible"
        class="vr-confirm-dialog"
        width="540px"
        :show-close="false"
        :close-on-click-modal="false"
        :close-on-press-escape="false"
        :append-to-body="true"
        align-center
      >
        <div class="vr-confirm-content">
          <div class="vr-confirm-heading">
            <span class="vr-confirm-icon" :class="state.variant">
              {{ state.variant === 'success' ? '✓' : state.variant === 'warning' ? '!' : 'i' }}
            </span>
            <div>
              <h3>{{ state.title }}</h3>
              <p v-if="state.message">{{ state.message }}</p>
            </div>
          </div>

          <div v-if="state.steps.length" class="vr-confirm-steps">
            <div v-for="(step, index) in state.steps" :key="index" class="vr-confirm-step">
              <span>{{ index + 1 }}</span><p>{{ step }}</p>
            </div>
          </div>

          <el-alert
            v-if="state.alertTitle || state.alertMessage"
            class="vr-confirm-alert"
            :title="state.alertTitle"
            :description="state.alertMessage"
            type="warning"
            :closable="false"
            show-icon
          />

          <div v-if="state.requireAcknowledge" class="vr-confirm-acknowledge">
            <el-checkbox v-model="state.acknowledged">{{ state.acknowledgeText }}</el-checkbox>
          </div>
        </div>
        <template #footer>
          <div class="vr-confirm-actions">
            <el-button @click="cancel">{{ state.cancelText }}</el-button>
            <el-button type="primary" :disabled="state.requireAcknowledge && !state.acknowledged" @click="accept">{{ state.confirmText }}</el-button>
          </div>
        </template>
      </el-dialog>
    `
  });

  app.use(ElementPlus);
  app.mount("#componentLayer");

  enhanceSelects();
  const selectObserver = new MutationObserver((records) => {
    records.forEach((record) => {
      record.removedNodes.forEach((node) => {
        if (node instanceof Element && !node.isConnected) destroySelects(node);
      });
      record.addedNodes.forEach((node) => {
        if (node instanceof Element) enhanceSelects(node);
      });
    });
  });
  selectObserver.observe(document.body, {childList: true, subtree: true});
})();
