(() => {
  const storageKey = "mdocman.preview.theme";
  const switcher = document.querySelector(".theme-switcher");
  const toggle = document.querySelector("[data-preview-theme-toggle]");
  const menu = document.querySelector("[data-preview-theme-menu]");
  const options = Array.from(
    document.querySelectorAll("[data-preview-theme-option]"),
  );
  const currentTheme = document.querySelector('meta[name="mdoc-theme"]')?.content || "default";

  if (
    !(switcher instanceof HTMLElement) ||
    !(toggle instanceof HTMLButtonElement) ||
    !(menu instanceof HTMLElement)
  ) {
    return;
  }

  const supportedThemes = new Set(
    options.map((option) => option.dataset.previewThemeOption),
  );

  const applyTheme = (theme, persist = true) => {
    const selected = supportedThemes.has(theme) ? theme : currentTheme;
    document.documentElement.dataset.previewTheme = selected;
    if (selected !== currentTheme) {
      const target = new URL(location.href);
      target.searchParams.set("theme", selected);
      document.cookie = `mdoc-theme=${encodeURIComponent(selected)}; Path=/; SameSite=Lax; Max-Age=31536000`;
      const form = document.querySelector("[data-preview-input]");
      if (form instanceof HTMLFormElement) {
        form.action = target.href;
        form.requestSubmit();
      } else {
        location.replace(target.href);
      }
    }
    if (persist) {
      try {
        localStorage.setItem(storageKey, selected);
      } catch {
        // The preview still works when storage is disabled.
      }
    }
  };

  const setMenuOpen = (open, focusSelected = false) => {
    menu.hidden = !open;
    toggle.setAttribute("aria-expanded", String(open));
    if (open && focusSelected) {
      options
        .find((option) => option.getAttribute("aria-checked") === "true")
        ?.focus();
    }
  };

  let savedTheme = currentTheme;
  try {
    savedTheme = localStorage.getItem(storageKey) || savedTheme;
  } catch {
    // Use the default theme when storage is disabled.
  }

  applyTheme(new URL(location.href).searchParams.has("theme") ? currentTheme : savedTheme, false);
  toggle.addEventListener("click", () => {
    setMenuOpen(menu.hidden, menu.hidden);
  });
  options.forEach((option) => {
    option.addEventListener("click", () => {
      applyTheme(option.dataset.previewThemeOption);
      setMenuOpen(false);
      toggle.focus();
    });
  });
  document.addEventListener("click", (event) => {
    if (event.target instanceof Node && !switcher.contains(event.target)) {
      setMenuOpen(false);
    }
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !menu.hidden) {
      setMenuOpen(false);
      toggle.focus();
    }
  });
})();

(() => {
  document.querySelectorAll("[data-path-tree-id]").forEach((tree) => {
  if (!(tree instanceof HTMLElement) || !tree.dataset.pathTreeId) {
    return;
  }

  const storageKey = `mdocman.preview.tree.${tree.dataset.pathTreeId}`;
  const scrollStorageKey = `${storageKey}.scroll`;
  const directories = Array.from(
    tree.querySelectorAll("details[data-path-tree-key]"),
  );

  const readState = () => {
    try {
      const value = JSON.parse(localStorage.getItem(storageKey) || "{}");
      return value && typeof value === "object" ? value : {};
    } catch {
      return {};
    }
  };

  const savedState = readState();
  directories.forEach((directory) => {
    const directoryKey = directory.dataset.pathTreeKey;
    if (!directoryKey) {
      return;
    }
    const stateKey = `node:${directoryKey}`;
    if (typeof savedState[stateKey] === "boolean") {
      directory.open = savedState[stateKey];
    }
    directory.addEventListener("toggle", () => {
      const nextState = readState();
      nextState[stateKey] = directory.open;
      try {
        localStorage.setItem(storageKey, JSON.stringify(nextState));
      } catch {
        // Navigation still works when storage is disabled.
      }
    });
  });

  const readScrollTop = () => {
    try {
      const storedValue = sessionStorage.getItem(scrollStorageKey);
      if (storedValue === null) {
        return null;
      }
      const value = Number(storedValue);
      return Number.isFinite(value) && value >= 0 ? value : null;
    } catch {
      return null;
    }
  };

  const saveScrollTop = () => {
    try {
      sessionStorage.setItem(scrollStorageKey, String(tree.scrollTop));
    } catch {
      // Navigation still works when storage is disabled.
    }
  };

  const savedScrollTop = readScrollTop();
  if (savedScrollTop !== null) {
    requestAnimationFrame(() => {
      tree.scrollTop = savedScrollTop;
    });
  }

  let scrollFrame = 0;
  tree.addEventListener(
    "scroll",
    () => {
      if (scrollFrame) {
        return;
      }
      scrollFrame = requestAnimationFrame(() => {
        scrollFrame = 0;
        saveScrollTop();
      });
    },
    { passive: true },
  );
  tree.addEventListener("click", (event) => {
    if (event.target instanceof Element && event.target.closest("a[href]")) {
      saveScrollTop();
    }
  });
  window.addEventListener("pagehide", saveScrollTop);
  });
})();
