(() => {
  const storageKey = "mdocman.preview.theme";
  const switcher = document.querySelector(".theme-switcher");
  const toggle = document.querySelector("[data-preview-theme-toggle]");
  const menu = document.querySelector("[data-preview-theme-menu]");
  const options = Array.from(
    document.querySelectorAll("[data-preview-theme-option]"),
  );
  const stylesheet = document.querySelector("#preview-theme-stylesheet");

  if (
    !(switcher instanceof HTMLElement) ||
    !(toggle instanceof HTMLButtonElement) ||
    !(menu instanceof HTMLElement) ||
    !(stylesheet instanceof HTMLLinkElement)
  ) {
    return;
  }

  const supportedThemes = new Set(
    options.map((option) => option.dataset.previewThemeOption),
  );

  const applyTheme = (theme, persist = true) => {
    const selected = supportedThemes.has(theme) ? theme : "default";
    document.documentElement.dataset.previewTheme = selected;
    stylesheet.href = `/_mdoc/themes/${selected}.css`;
    options.forEach((option) => {
      option.setAttribute(
        "aria-checked",
        String(option.dataset.previewThemeOption === selected),
      );
    });
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

  let savedTheme = "default";
  try {
    savedTheme = localStorage.getItem(storageKey) || savedTheme;
  } catch {
    // Use the default theme when storage is disabled.
  }

  applyTheme(savedTheme, false);
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
  const tree = document.querySelector("[data-path-tree-id]");
  if (!(tree instanceof HTMLElement) || !tree.dataset.pathTreeId) {
    return;
  }

  const storageKey = `mdocman.preview.tree.${tree.dataset.pathTreeId}`;
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
})();
