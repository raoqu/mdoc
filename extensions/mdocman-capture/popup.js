const token = document.querySelector("#token");
const note = document.querySelector("#note");
const capture = document.querySelector("#capture");
const status = document.querySelector("#status");
const queue = document.querySelector("#queue");

const stored = await chrome.storage.local.get({ token: "", queue: [] });
token.value = stored.token;
queue.textContent = stored.queue.length ? `${stored.queue.length} 条排队` : "队列为空";

token.addEventListener("change", async () => {
  await chrome.storage.local.set({ token: token.value.trim() });
  void chrome.runtime.sendMessage({ type: "flush" });
});

capture.addEventListener("click", async () => {
  if (!token.value.trim()) {
    status.textContent = "请先粘贴墨笺设置中创建的扩展令牌。";
    return;
  }
  await chrome.storage.local.set({ token: token.value.trim() });
  capture.disabled = true;
  status.textContent = "正在捕获页面…";
  const result = await chrome.runtime.sendMessage({ type: "capture", note: note.value.trim() });
  capture.disabled = false;
  if (result.error && result.queued === undefined) {
    status.textContent = result.error;
  } else if (result.queued > 0) {
    status.textContent = `已排队；本机服务可用后自动重试。${result.error ? ` ${result.error}` : ""}`;
  } else {
    status.textContent = "已保存到今天的每日笔记。";
  }
  queue.textContent = result.queued ? `${result.queued} 条排队` : "队列为空";
});
