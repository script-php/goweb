(() => {
  const body = document.body;
  const sidebar = document.querySelector(".admin-sidebar");
  const openButton = document.querySelector("[data-menu-open]");
  const closeButtons = document.querySelectorAll("[data-menu-close]");
  const mobile = window.matchMedia("(max-width: 800px)");

  if (!sidebar || !openButton) return;

  const setOpen = (open) => {
    body.classList.toggle("menu-open", open);
    openButton.setAttribute("aria-expanded", String(open));
    if (open) {
      sidebar.querySelector("a")?.focus();
    } else if (mobile.matches) {
      openButton.focus();
    }
  };

  openButton.addEventListener("click", () => setOpen(true));
  closeButtons.forEach((button) => button.addEventListener("click", () => setOpen(false)));
  sidebar.querySelectorAll("a").forEach((link) => {
    if (link.pathname === window.location.pathname) link.setAttribute("aria-current", "page");
    link.addEventListener("click", () => {
      if (mobile.matches) setOpen(false);
    });
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && body.classList.contains("menu-open")) setOpen(false);
  });
  mobile.addEventListener("change", (event) => {
    if (!event.matches) setOpen(false);
  });

  const confirmDialog = document.querySelector("[data-confirm-dialog]");
  const confirmMessage = confirmDialog?.querySelector("[data-confirm-message]");
  let pendingForm = null;
  document.querySelectorAll("form[data-confirm]").forEach((form) => {
    form.addEventListener("submit", (event) => {
      if (!confirmDialog || form.dataset.confirmed === "true") return;
      event.preventDefault();
      pendingForm = form;
      confirmMessage.textContent = form.dataset.confirm;
      confirmDialog.showModal();
    });
  });
  confirmDialog?.addEventListener("close", () => {
    if (confirmDialog.returnValue === "confirm" && pendingForm) {
      pendingForm.dataset.confirmed = "true";
      pendingForm.requestSubmit();
    }
    pendingForm = null;
  });

  document.querySelectorAll("[data-permission-group]").forEach((group) => {
    const boxes = group.querySelectorAll('input[name="permissions"]:not(:disabled)');
    group.querySelector("[data-permission-select]")?.addEventListener("click", () => boxes.forEach((box) => { box.checked = true; }));
    group.querySelector("[data-permission-clear]")?.addEventListener("click", () => boxes.forEach((box) => { box.checked = false; }));
  });

  document.querySelectorAll("[data-avatar-input]").forEach((input) => {
    input.addEventListener("change", () => {
      const file = input.files?.[0];
      const preview = document.querySelector("[data-avatar-preview]");
      if (!file || !preview) return;
      const image = document.createElement("img");
      image.alt = "Selected avatar preview";
      image.src = URL.createObjectURL(file);
      image.addEventListener("load", () => URL.revokeObjectURL(image.src), { once: true });
      preview.replaceChildren(image);
      input.closest("[data-avatar-form]")?.requestSubmit();
    });
  });
})();
