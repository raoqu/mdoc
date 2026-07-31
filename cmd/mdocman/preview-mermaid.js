import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";

const MIN_SCALE = 0.1;
const MAX_SCALE = 8;
const SCALE_STEP = 0.15;
/** Padding (px) kept around the diagram when fitting to the stage. */
const FIT_PADDING = 32;

const expandIcon = `<svg aria-hidden="true" viewBox="0 0 24 24"><path d="M9 3H5a2 2 0 0 0-2 2v4"></path><path d="M15 3h4a2 2 0 0 1 2 2v4"></path><path d="M21 15v4a2 2 0 0 1-2 2h-4"></path><path d="M3 15v4a2 2 0 0 0 2 2h4"></path></svg>`;

/** @type {((sourceDiagram: HTMLElement) => void) | null} */
let openLightbox = null;

const nodes = [];
document.querySelectorAll("pre > code.language-mermaid").forEach((code) => {
  const pre = code.parentElement;
  if (!(pre instanceof HTMLElement)) {
    return;
  }
  const wrap = document.createElement("div");
  wrap.className = "mermaid-wrap";

  const diagram = document.createElement("div");
  diagram.className = "mermaid";
  diagram.textContent = code.textContent ?? "";

  const expand = document.createElement("button");
  expand.type = "button";
  expand.className = "mermaid-expand";
  expand.setAttribute("aria-label", "在浮层中预览并缩放");
  expand.title = "放大预览";
  expand.innerHTML = expandIcon;

  wrap.append(diagram, expand);
  pre.replaceWith(wrap);
  nodes.push({ wrap, diagram, expand });
});

if (nodes.length === 0) {
  // Nothing to render.
} else {
  const theme =
    document.documentElement.dataset.previewTheme === "dark" ? "dark" : "neutral";
  mermaid.initialize({
    startOnLoad: false,
    theme,
    securityLevel: "strict",
  });
  mermaid
    .run({ nodes: nodes.map((item) => item.diagram) })
    .then(() => {
      openLightbox = createMermaidLightbox();
      nodes.forEach(enhanceMermaidBlock);
    })
    .catch((error) => {
      console.error("Mermaid render failed:", error);
    });
}

/**
 * @param {{ wrap: HTMLElement, diagram: HTMLElement, expand: HTMLButtonElement }} item
 */
function enhanceMermaidBlock(item) {
  const { wrap, diagram, expand } = item;
  if (!diagram.querySelector("svg")) {
    expand.hidden = true;
    return;
  }
  expand.addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    openLightbox?.(diagram);
  });
  wrap.addEventListener("dblclick", (event) => {
    if (event.target instanceof Element && event.target.closest("button")) {
      return;
    }
    openLightbox?.(diagram);
  });
}

/**
 * @returns {(sourceDiagram: HTMLElement) => void}
 */
function createMermaidLightbox() {
  const existing = document.querySelector(".mermaid-lightbox");
  if (existing instanceof HTMLElement) {
    existing.remove();
  }

  const lightbox = document.createElement("div");
  lightbox.className = "mermaid-lightbox";
  lightbox.hidden = true;
  lightbox.setAttribute("role", "dialog");
  lightbox.setAttribute("aria-modal", "true");
  lightbox.setAttribute("aria-label", "Mermaid 图表预览");
  lightbox.innerHTML = `
    <div class="mermaid-lightbox-backdrop" data-mermaid-close></div>
    <div class="mermaid-lightbox-panel">
      <div class="mermaid-lightbox-toolbar">
        <span class="mermaid-lightbox-scale" data-mermaid-scale>100%</span>
        <div class="mermaid-lightbox-actions">
          <button type="button" class="mermaid-lightbox-btn" data-mermaid-zoom-out aria-label="缩小" title="缩小">−</button>
          <button type="button" class="mermaid-lightbox-btn" data-mermaid-zoom-in aria-label="放大" title="放大">+</button>
          <button type="button" class="mermaid-lightbox-btn" data-mermaid-zoom-reset aria-label="适应画布" title="适应画布">适应</button>
          <button type="button" class="mermaid-lightbox-btn mermaid-lightbox-close" data-mermaid-close aria-label="关闭" title="关闭">×</button>
        </div>
      </div>
      <div class="mermaid-lightbox-stage" data-mermaid-stage>
        <div class="mermaid-lightbox-canvas" data-mermaid-canvas></div>
      </div>
      <p class="mermaid-lightbox-hint">滚轮缩放 · 拖拽平移 · Esc 关闭</p>
    </div>
  `;
  document.body.append(lightbox);

  /** @type {{ scale: number, fitScale: number, x: number, y: number, dragging: boolean, lastX: number, lastY: number, pointerId: number | null }} */
  const state = {
    scale: 1,
    fitScale: 1,
    x: 0,
    y: 0,
    dragging: false,
    lastX: 0,
    lastY: 0,
    pointerId: null,
  };

  const stage = lightbox.querySelector("[data-mermaid-stage]");
  const canvas = lightbox.querySelector("[data-mermaid-canvas]");
  const scaleLabel = lightbox.querySelector("[data-mermaid-scale]");
  if (
    !(stage instanceof HTMLElement) ||
    !(canvas instanceof HTMLElement) ||
    !(scaleLabel instanceof HTMLElement)
  ) {
    return () => {};
  }

  const applyTransform = () => {
    canvas.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.scale})`;
    scaleLabel.textContent = `${Math.round(state.scale * 100)}%`;
  };

  /**
   * Measure the SVG at the current transform and compute a scale that fits
   * the stage (contain), then center the diagram.
   */
  const fitToStage = () => {
    const svg = canvas.querySelector("svg");
    if (!(svg instanceof SVGElement)) {
      state.scale = 1;
      state.fitScale = 1;
      state.x = 0;
      state.y = 0;
      applyTransform();
      return;
    }

    // Measure natural size relative to the current scale so fit is stable.
    const stageRect = stage.getBoundingClientRect();
    const svgRect = svg.getBoundingClientRect();
    const naturalW = svgRect.width / (state.scale || 1);
    const naturalH = svgRect.height / (state.scale || 1);
    if (naturalW <= 0 || naturalH <= 0 || stageRect.width <= 0 || stageRect.height <= 0) {
      state.scale = 1;
      state.fitScale = 1;
      state.x = 0;
      state.y = 0;
      applyTransform();
      return;
    }

    const availW = Math.max(1, stageRect.width - FIT_PADDING * 2);
    const availH = Math.max(1, stageRect.height - FIT_PADDING * 2);
    const fit = Math.min(availW / naturalW, availH / naturalH);
    state.fitScale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, fit));
    state.scale = state.fitScale;
    state.x = 0;
    state.y = 0;
    applyTransform();
  };

  /**
   * Zoom around a point in stage coordinates (origin at stage top-left).
   * Canvas is positioned at stage center (top/left 50%), so zoom math must
   * account for that offset.
   * @param {number} next
   * @param {number} [originX] clientX
   * @param {number} [originY] clientY
   */
  const setScale = (next, originX, originY) => {
    const clamped = Math.min(MAX_SCALE, Math.max(MIN_SCALE, next));
    if (clamped === state.scale) {
      applyTransform();
      return;
    }
    if (typeof originX === "number" && typeof originY === "number") {
      const rect = stage.getBoundingClientRect();
      const px = originX - rect.left;
      const py = originY - rect.top;
      const cx = rect.width / 2;
      const cy = rect.height / 2;
      // Keep the stage point under the cursor stable while zooming.
      state.x = px - cx - ((px - cx - state.x) * clamped) / state.scale;
      state.y = py - cy - ((py - cy - state.y) * clamped) / state.scale;
    }
    state.scale = clamped;
    applyTransform();
  };

  const close = () => {
    lightbox.hidden = true;
    document.body.classList.remove("mermaid-lightbox-open");
    canvas.replaceChildren();
    state.dragging = false;
    state.pointerId = null;
  };

  const open = (/** @type {HTMLElement} */ sourceDiagram) => {
    const svg = sourceDiagram.querySelector("svg");
    if (!(svg instanceof SVGElement)) {
      return;
    }
    const clone = /** @type {SVGElement} */ (svg.cloneNode(true));
    canvas.replaceChildren(clone);
    state.scale = 1;
    state.fitScale = 1;
    state.x = 0;
    state.y = 0;
    applyTransform();
    lightbox.hidden = false;
    document.body.classList.add("mermaid-lightbox-open");
    // Wait for layout so stage metrics are correct, then fill the canvas.
    requestAnimationFrame(() => {
      fitToStage();
      // Second frame handles late SVG layout (viewBox / fonts).
      requestAnimationFrame(fitToStage);
    });
    const closeBtn = lightbox.querySelector(".mermaid-lightbox-close");
    if (closeBtn instanceof HTMLElement) {
      closeBtn.focus();
    }
  };

  lightbox.querySelectorAll("[data-mermaid-close]").forEach((el) => {
    el.addEventListener("click", close);
  });
  lightbox.querySelector("[data-mermaid-zoom-in]")?.addEventListener("click", () => {
    setScale(state.scale + SCALE_STEP);
  });
  lightbox.querySelector("[data-mermaid-zoom-out]")?.addEventListener("click", () => {
    setScale(state.scale - SCALE_STEP);
  });
  lightbox.querySelector("[data-mermaid-zoom-reset]")?.addEventListener("click", () => {
    fitToStage();
  });

  stage.addEventListener(
    "wheel",
    (event) => {
      event.preventDefault();
      event.stopPropagation();
      // Multiplicative zoom feels smoother on trackpads (pixel deltas).
      const intensity = Math.min(0.35, Math.abs(event.deltaY) / 200);
      const factor = event.deltaY > 0 ? 1 - intensity : 1 + intensity;
      // Fall back to fixed steps for coarse mouse-wheel ticks.
      const next =
        Math.abs(event.deltaY) >= 40
          ? state.scale + (event.deltaY > 0 ? -SCALE_STEP : SCALE_STEP)
          : state.scale * factor;
      setScale(next, event.clientX, event.clientY);
    },
    { passive: false },
  );

  // Also catch wheel on the panel so zoom works over toolbar/hint edges.
  lightbox.querySelector(".mermaid-lightbox-panel")?.addEventListener(
    "wheel",
    (event) => {
      if (!(event.target instanceof Element)) {
        return;
      }
      // Stage already handles its own wheel; skip double-handling.
      if (event.target.closest("[data-mermaid-stage]")) {
        return;
      }
      event.preventDefault();
      const intensity = Math.min(0.35, Math.abs(event.deltaY) / 200);
      const factor = event.deltaY > 0 ? 1 - intensity : 1 + intensity;
      const next =
        Math.abs(event.deltaY) >= 40
          ? state.scale + (event.deltaY > 0 ? -SCALE_STEP : SCALE_STEP)
          : state.scale * factor;
      setScale(next);
    },
    { passive: false },
  );

  stage.addEventListener("pointerdown", (event) => {
    if (event.button !== 0) {
      return;
    }
    state.dragging = true;
    state.pointerId = event.pointerId;
    state.lastX = event.clientX;
    state.lastY = event.clientY;
    stage.setPointerCapture(event.pointerId);
    stage.classList.add("is-dragging");
  });
  stage.addEventListener("pointermove", (event) => {
    if (!state.dragging || state.pointerId !== event.pointerId) {
      return;
    }
    state.x += event.clientX - state.lastX;
    state.y += event.clientY - state.lastY;
    state.lastX = event.clientX;
    state.lastY = event.clientY;
    applyTransform();
  });
  const endDrag = (/** @type {PointerEvent} */ event) => {
    if (state.pointerId !== event.pointerId) {
      return;
    }
    state.dragging = false;
    state.pointerId = null;
    stage.classList.remove("is-dragging");
  };
  stage.addEventListener("pointerup", endDrag);
  stage.addEventListener("pointercancel", endDrag);

  document.addEventListener("keydown", (event) => {
    if (lightbox.hidden) {
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      close();
      return;
    }
    if (event.key === "+" || event.key === "=") {
      event.preventDefault();
      setScale(state.scale + SCALE_STEP);
    } else if (event.key === "-" || event.key === "_") {
      event.preventDefault();
      setScale(state.scale - SCALE_STEP);
    } else if (event.key === "0") {
      event.preventDefault();
      fitToStage();
    }
  });

  window.addEventListener("resize", () => {
    if (lightbox.hidden) {
      return;
    }
    // Only re-fit when the user hasn't panned/zoomed away from the default fit.
    if (
      Math.abs(state.scale - state.fitScale) < 0.001 &&
      Math.abs(state.x) < 0.5 &&
      Math.abs(state.y) < 0.5
    ) {
      fitToStage();
    }
  });

  return open;
}
