(() => {
  const {createApp, reactive} = Vue;

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
})();
