(() => {
  "use strict";

  const state = { surface: null, trigger: null, dialog: null, pending: null, allowed: null };
  const surfaceTriggers = new WeakMap();

  function focusable(root) {
    return root.querySelector(
      '[data-surface-focus], input:not([type="hidden"]), select, textarea, button:not(.surface__close), a[href], [tabindex]:not([tabindex="-1"])',
    );
  }

  function focusSurface(surface) {
    const target = focusable(surface.querySelector(".surface__content") || surface) || surface;
    target.focus({ preventScroll: true });
  }

  function formSnapshot(form) {
    return new URLSearchParams(new FormData(form)).toString();
  }

  function rememberForms(surface) {
    surface.querySelectorAll("form").forEach((form) => {
      form.dataset.surfaceInitial = formSnapshot(form);
    });
  }

  function isDirty(surface) {
    return [...surface.querySelectorAll("form")].some((form) =>
      form.dataset.surfaceInitial !== undefined && form.dataset.surfaceInitial !== formSnapshot(form),
    );
  }

  function setSurfaceVisible(surface, visible) {
    surface.hidden = !visible;
    surface.classList.toggle("is-open", visible);
    surface.setAttribute("aria-hidden", String(!visible));
  }

  function setBackdropVisible(visible) {
    const backdrop = document.querySelector("[data-surface-backdrop]");
    if (!backdrop) return;
    backdrop.hidden = !visible;
  }

  function closeSurface(surface, force = false) {
    if (!surface || surface.hidden) return true;
    if (!force && isDirty(surface)) {
      openConfirmation("Discard unsaved changes?", () => closeSurface(surface, true));
      return false;
    }

    setSurfaceVisible(surface, false);
    if (state.surface === surface) {
      state.surface = null;
      setBackdropVisible(false);
      document.body.classList.remove("has-surface-open");
      const trigger = state.trigger;
      state.trigger = null;
      if (trigger && trigger.isConnected) trigger.focus({ preventScroll: true });
    }
    return true;
  }

  function openSurface(surface, trigger) {
    if (!surface) return;
    if (state.surface && state.surface !== surface) closeSurface(state.surface, true);
    state.surface = surface;
    state.trigger = trigger || surfaceTriggers.get(surface) || null;
    surfaceTriggers.set(surface, state.trigger);
    setBackdropVisible(true);
    document.body.classList.add("has-surface-open");
    setSurfaceVisible(surface, true);
    rememberForms(surface);
    focusSurface(surface);
  }

  function closeDialog() {
    const dialog = state.dialog;
    if (!dialog) return;
    if (dialog.open && typeof dialog.close === "function") dialog.close();
    else dialog.removeAttribute("open");
    dialog.hidden = true;
    state.dialog = null;
    state.pending = null;
  }

  function openConfirmation(message, action) {
    const dialog = document.querySelector("#confirmation-dialog");
    if (!dialog) return;
    const messageNode = dialog.querySelector("#confirmation-dialog-message");
    if (messageNode) messageNode.textContent = message;
    state.dialog = dialog;
    state.pending = { action, trigger: document.activeElement };
    dialog.hidden = false;
    if (typeof dialog.showModal === "function") dialog.showModal();
    else dialog.setAttribute("open", "");
    dialog.querySelector("[data-confirm-accept]").focus({ preventScroll: true });
  }

  function confirmRequest(element) {
    openConfirmation(element.dataset.confirm || "Continue?", null);
    if (state.pending) state.pending.element = element;
  }

  function acceptConfirmation() {
    const pending = state.pending;
    const trigger = pending && pending.trigger;
    const element = pending && pending.element;
    const action = pending && pending.action;
    closeDialog();
    if (action) {
      action();
    } else if (element && element.isConnected) {
      state.allowed = element;
      if (element.matches("form")) element.requestSubmit();
      else element.click();
      queueMicrotask(() => {
        if (state.allowed === element) state.allowed = null;
      });
    }
    if (trigger && trigger.isConnected && !state.surface) trigger.focus({ preventScroll: true });
  }

  function isAllowed(element) {
    if (state.allowed !== element) return false;
    state.allowed = null;
    return true;
  }

  function closeRequestedSurface(element) {
    const surface = element.closest(".surface");
    closeSurface(surface);
  }

  document.addEventListener("click", (event) => {
    const target = event.target.closest("[data-confirm]");
    if (target && !isAllowed(target)) {
      event.preventDefault();
      event.stopPropagation();
      confirmRequest(target);
      return;
    }

    const opener = event.target.closest("[data-open-surface]");
    if (opener) {
      const surface = document.querySelector(opener.dataset.openSurface);
      if (surface) openSurface(surface, opener);
      return;
    }

    if (event.target.closest("[data-close-surface]")) {
      closeRequestedSurface(event.target);
      return;
    }

    if (event.target.matches("[data-surface-backdrop]")) closeSurface(state.surface);

    if (event.target.closest("[data-confirm-cancel]")) {
      const trigger = state.pending && state.pending.trigger;
      closeDialog();
      if (trigger && trigger.isConnected) trigger.focus({ preventScroll: true });
      return;
    }

    if (event.target.closest("[data-confirm-accept]")) acceptConfirmation();
  }, true);

  document.addEventListener("submit", (event) => {
    const form = event.target.closest("form[data-confirm]");
    if (!form || isAllowed(form)) return;
    event.preventDefault();
    event.stopPropagation();
    confirmRequest(form);
  }, true);

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") return;
    if (state.dialog) {
      event.preventDefault();
      const trigger = state.pending && state.pending.trigger;
      closeDialog();
      if (trigger && trigger.isConnected) trigger.focus({ preventScroll: true });
      return;
    }
    if (state.surface) {
      event.preventDefault();
      closeSurface(state.surface);
    }
  });

  document.addEventListener("htmx:afterSwap", (event) => {
    const target = event.detail && event.detail.target;
    const surface = target && target.closest && target.closest(".surface");
    if (!surface) return;
    rememberForms(surface);
    focusSurface(surface);
  });

  document.addEventListener("htmx:afterRequest", (event) => {
    const detail = event.detail || {};
    const source = detail.elt;
    if (!detail.successful || !source) return;
    const surface = source.closest && source.closest(".surface");
    if (surface && (source.dataset.closeOnSuccess === "true" || surface.dataset.closeOnSuccess === "true")) {
      closeSurface(surface, true);
    }
  });

  document.addEventListener("cancel", (event) => {
    if (event.target.matches("#confirmation-dialog")) {
      event.preventDefault();
      const trigger = state.pending && state.pending.trigger;
      closeDialog();
      if (trigger && trigger.isConnected) trigger.focus({ preventScroll: true });
    }
  });

  window.surfaceController = { open: openSurface, close: closeSurface, confirm: openConfirmation };
})();
