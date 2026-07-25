const API = "http://127.0.0.1:8080/api/capture";

async function state() {
  return chrome.storage.local.get({ token: "", queue: [] });
}

async function flushQueue() {
  const current = await state();
  if (!current.token || current.queue.length === 0) return { sent: 0, queued: current.queue.length };
  const remaining = [];
  let sent = 0;
  let lastError = "";
  for (const item of current.queue) {
    try {
      const response = await fetch(API, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${current.token}` },
        body: JSON.stringify(item),
      });
      if (!response.ok) throw new Error(await response.text());
      sent += 1;
    } catch (error) {
      remaining.push(item);
      lastError = error instanceof Error ? error.message : "本机服务不可用";
    }
  }
  await chrome.storage.local.set({ queue: remaining, lastError, lastFlush: Date.now() });
  return { sent, queued: remaining.length, error: lastError };
}

async function capturePage(note = "") {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.id || !tab.url?.startsWith("http")) throw new Error("当前页面不能被捕获");
  const [{ result: selection = "" }] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: () => window.getSelection()?.toString() ?? "",
  });
  let screenshot = "";
  try {
    screenshot = await chrome.tabs.captureVisibleTab(tab.windowId, { format: "jpeg", quality: 72 });
  } catch {
    // Restricted pages can still be captured without an image.
  }
  const current = await state();
  const item = {
    url: tab.url,
    title: tab.title || tab.url,
    selection,
    screenshot,
    note,
    capturedAt: new Date().toISOString(),
    source: "chrome-extension"
  };
  await chrome.storage.local.set({ queue: [...current.queue, item] });
  return flushQueue();
}

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  const operation = message?.type === "capture" ? capturePage(message.note) : flushQueue();
  operation.then(sendResponse).catch((error) => sendResponse({ error: error.message }));
  return true;
});

chrome.commands.onCommand.addListener((command) => {
  if (command === "capture-page") void capturePage();
});

chrome.runtime.onInstalled.addListener(() => chrome.alarms.create("retry-captures", { periodInMinutes: 1 }));
chrome.runtime.onStartup.addListener(() => void flushQueue());
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === "retry-captures") void flushQueue();
});
