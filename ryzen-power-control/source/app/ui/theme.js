(() => {
  const tokens = [
    "--semi-color-app", "--semi-color-Component-card", "--semi-color-card-HoverBg", "--semi-color-text-0",
    "--semi-color-text-1", "--semi-color-text-2", "--semi-color-text-3", "--semi-color-mode-minor-text",
    "--semi-color-border", "--semi-color-primary", "--semi-color-primary-hover",
    "--semi-color-danger", "--semi-color-danger-hover", "--semi-color-success",
    "--semi-color-warning", "--semi-color-fill-0", "--semi-color-fill-1",
    "--semi-color-bg-1", "--semi-color-input-DefaultBg", "--semi-color-disabled-text",
    "--semi-color-progress-defaultBg", "--semi-color-success-light-default",
    "--semi-color-danger-light-default", "--semi-color-warning-light-default",
    "--semi-color-focus-border", "--semi-color-bg-2", "--semi-color-overlay-bg",
    "--semi-color-overlay-CustomAnti", "--semi-color-border-modal", "--font-family-regular",
    "--semi-border-radius-custom-button-default", "--semi-border-radius-custom-button-small",
    "--semi-color-primary-active", "--semi-color-primary-light-active", "--semi-color-tertiary",
    "--semi-color-tertiary-hover", "--semi-color-tertiary-active", "--semi-color-fill-2", "--semi-white",
    "--semi-border-radius-small", "--semi-border-radius-large", "--semi-shadow-elevated",
    "--semi-transition_duration-none", "--semi-transition_function-easeIn",
    "--semi-transition_delay-none", "--semi-transform_scale-none",
  ];

  const root = document.documentElement;
  const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");
  let hostWindow = null;
  let host = null;

  try {
    hostWindow = window.parent;
    host = hostWindow.document;
  } catch (error) {
    console.warn("无法读取飞牛宿主主题，将跟随系统主题", error);
  }

  function syncTheme() {
    let hostMode = "";
    let hostIsDark = false;
    if (host) {
      try {
        const hostStyle = hostWindow.getComputedStyle(host.body);
        for (const token of tokens) {
          const value = hostStyle.getPropertyValue(token).trim();
          if (value) root.style.setProperty(token, value);
          else root.style.removeProperty(token);
        }
        hostMode = host.body.getAttribute("theme-mode") || host.documentElement.getAttribute("theme-mode") || "";
        hostIsDark = host.body.classList.contains("dark") || host.documentElement.classList.contains("dark");
      } catch (error) {
        host = null;
        console.warn("无法读取飞牛宿主主题，将跟随系统主题", error);
      }
    }

    const explicitMode = hostMode === "dark" || hostMode === "light" ? hostMode : "";
    const mode = explicitMode || (hostIsDark || systemTheme.matches ? "dark" : "light");
    root.dataset.theme = mode;
    root.style.colorScheme = mode;
    root.dataset.themeFallback = String(!explicitMode && !hostIsDark);
  }

  syncTheme();
  if (host) {
    const observer = new MutationObserver(syncTheme);
    observer.observe(host.body, { attributes: true, attributeFilter: ["theme-mode", "class", "style"] });
    observer.observe(host.documentElement, { attributes: true, attributeFilter: ["theme-mode", "class", "style"] });
  }
  systemTheme.addEventListener("change", syncTheme);
})();
