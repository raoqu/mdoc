import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const script = readFileSync(new URL("../cmd/mdocman/browser_activate_macos.js", import.meta.url), "utf8");

function browser(id, urls, active = false) {
  const tabs = urls.map((url) => Object.freeze({ url: () => url }));
  let activeTabIndex = 1;
  let currentTab = tabs[0];
  const window = { tabs: () => tabs, index: 2, minimized: true, miniaturized: true };
  Object.defineProperties(window, {
    activeTabIndex: { get: () => () => activeTabIndex, set: (value) => { activeTabIndex = value; } },
    currentTab: { get: () => () => currentTab, set: (value) => { currentTab = value; } },
  });
  return { id, active, window, activated: false };
}

function activate(browsers, target = "http://localhost:2026/") {
  const sandbox = {
    ObjC: { import() {}, unwrap: (value) => value },
    $: { NSWorkspace: { sharedWorkspace: { runningApplications: {
      count: browsers.length,
      objectAtIndex: (index) => ({ bundleIdentifier: browsers[index].id, active: browsers[index].active }),
    } } } },
    Application: (id) => {
      const instance = browsers.find((item) => item.id === id);
      return {
        windows: () => {
          if (instance.denied) throw new Error("Automation denied");
          return [instance.window];
        },
        activate: () => { instance.activated = true; },
      };
    },
  };
  vm.runInNewContext(script, sandbox);
  return sandbox.run([target]);
}

test("activates an existing document without changing its URL or opening another tab", () => {
  const documentURL = "http://127.0.0.1:2026/?note=research&view=editor#selection";
  const chrome = browser("com.google.Chrome", [
    "http://localhost:20260/",
    "http://localhost:2026/site/",
    "http://localhost:2026/_mdoc/knowledge/preview/",
    documentURL,
  ]);
  assert.equal(activate([chrome]), "activated");
  assert.equal(chrome.window.activeTabIndex(), 4);
  assert.equal(chrome.window.minimized, false);
  assert.equal(chrome.window.index, 1);
  assert.equal(chrome.activated, true);
  assert.equal(chrome.window.tabs().length, 4);
  assert.equal(chrome.window.tabs()[3].url(), documentURL);
});

test("Safari selects the existing tab and preserves document state", () => {
  const safari = browser("com.apple.Safari", ["about:blank", "http://[::1]:2026/?doc=draft"]);
  const original = safari.window.tabs()[1];
  assert.equal(activate([safari]), "activated");
  assert.equal(safari.window.currentTab(), original);
  assert.equal(safari.window.miniaturized, false);
  assert.equal(original.url(), "http://[::1]:2026/?doc=draft");
  assert.equal(safari.activated, true);
});

test("prefers the active browser when multiple workspaces are open", () => {
  const chrome = browser("com.google.Chrome", ["http://localhost:2026/"]);
  const edge = browser("com.microsoft.edgemac", ["http://localhost:2026/?doc=editing"], true);
  assert.equal(activate([chrome, edge]), "activated");
  assert.equal(edge.activated, true);
  assert.equal(chrome.activated, false);
});

test("keeps the selected document when several workspace tabs are open", () => {
  for (const id of ["com.google.Chrome", "com.apple.Safari"]) {
    const instance = browser(id, ["http://localhost:2026/?doc=old", "http://localhost:2026/?doc=editing"]);
    instance.window.activeTabIndex = 2;
    instance.window.currentTab = instance.window.tabs()[1];
    assert.equal(activate([instance]), "activated");
    assert.equal(instance.window.activeTabIndex(), 2);
    assert.equal(instance.window.currentTab(), instance.window.tabs()[1]);
  }
});

test("keeps unrelated ports, hosts, and preview pages untouched", () => {
  const chrome = browser("com.google.Chrome", [
    "http://localhost:20260/", "http://localhost.example:2026/",
    "https://localhost:2026/", "http://localhost:2026/site/",
    "http://localhost:2026/_mdoc/knowledge/preview/", "about:blank",
  ]);
  assert.equal(activate([chrome]), "not-found");
  assert.equal(chrome.activated, false);
  assert.equal(chrome.window.index, 2);
});

test("denied automation and unsupported browsers permit the OS fallback", () => {
  const chrome = browser("com.google.Chrome", ["http://localhost:2026/"]);
  chrome.denied = true;
  const firefox = browser("org.mozilla.firefox", ["http://localhost:2026/"]);
  assert.equal(activate([chrome, firefox]), "not-found");
  assert.equal(chrome.activated, false);
  assert.equal(firefox.activated, false);
});
