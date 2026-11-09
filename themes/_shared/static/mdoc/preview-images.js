(() => {
  const images = Array.from(document.querySelectorAll("img[data-preview-image]"))
    .filter((image) => !image.closest("a[href]"));
  if (images.length === 0) return;

  const dialog = document.createElement("dialog");
  dialog.className = "mdoc-image-preview";
  dialog.setAttribute("aria-label", "图片预览");
  dialog.innerHTML = `
    <div class="mdoc-image-preview-controls">
      <button type="button" data-previous aria-label="上一张">‹</button>
      <button type="button" data-next aria-label="下一张">›</button>
      <button type="button" data-zoom>原始大小</button>
      <button type="button" data-close aria-label="关闭图片预览" autofocus>×</button>
    </div>
    <div class="mdoc-image-preview-stage"><img alt=""></div>
    <p class="mdoc-image-preview-caption" aria-hidden="true"></p>
  `;
  document.body.append(dialog);

  const stage = dialog.querySelector(".mdoc-image-preview-stage");
  const fullImage = stage.querySelector("img");
  const caption = dialog.querySelector(".mdoc-image-preview-caption");
  const previous = dialog.querySelector("[data-previous]");
  const next = dialog.querySelector("[data-next]");
  const zoom = dialog.querySelector("[data-zoom]");
  let current = 0;

  const setZoom = (zoomed) => {
    stage.classList.toggle("is-zoomed", zoomed);
    zoom.textContent = zoomed ? "适应窗口" : "原始大小";
    stage.scrollTo(0, 0);
  };

  const showImage = (index) => {
    current = (index + images.length) % images.length;
    const image = images[current];
    fullImage.src = image.src;
    fullImage.alt = image.alt;
    caption.textContent = image.alt;
    caption.hidden = !image.alt;
    setZoom(false);
  };

  const toggleZoom = () => setZoom(!stage.classList.contains("is-zoomed"));
  zoom.addEventListener("click", toggleZoom);
  fullImage.addEventListener("click", toggleZoom);
  dialog.querySelector("[data-close]").addEventListener("click", () => dialog.close());
  dialog.addEventListener("click", (event) => {
    if (event.target === dialog || event.target === stage) dialog.close();
  });

  previous.hidden = next.hidden = images.length === 1;
  previous.addEventListener("click", () => showImage(current - 1));
  next.addEventListener("click", () => showImage(current + 1));
  dialog.addEventListener("keydown", (event) => {
    if (images.length === 1 || stage.classList.contains("is-zoomed")) return;
    if (event.key === "ArrowLeft") previous.click();
    if (event.key === "ArrowRight") next.click();
  });

  images.forEach((image, index) => {
    image.tabIndex = 0;
    image.setAttribute("role", "button");
    image.setAttribute("aria-haspopup", "dialog");
    image.setAttribute("aria-label", image.alt ? `预览图片：${image.alt}` : "预览图片");
    const open = () => {
      image.focus({ preventScroll: true });
      dialog.showModal();
      showImage(index);
    };
    image.addEventListener("click", open);
    image.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        open();
      }
    });
  });
})();
