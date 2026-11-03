// JXA runs outside the page: selecting a tab does not reload or navigate it.
function workspaceLocation(raw) {
  var match = /^(https?):\/\/(localhost|127\.0\.0\.1|\[::1\]):(\d+)(\/[^?#]*)?(?:[?#].*)?$/i.exec(raw);
  if (match) return match[1].toLowerCase() + "://loopback:" + Number(match[3]) + (match[4] || "/");
  return raw.split(/[?#]/)[0];
}

function activateTab(browser, browserWindow, tab, index, kind, target) {
  if (!tab) return false;
  var url = tab.url();
  // Ignore query/hash state so the current document stays selected.
  // Match the path exactly to avoid selecting a publication/preview tab.
  if (typeof url !== "string" || workspaceLocation(url) !== target) return false;
  if (kind === "safari") {
    browserWindow.currentTab = tab;
    browserWindow.miniaturized = false;
  } else {
    browserWindow.activeTabIndex = index;
    browserWindow.minimized = false;
  }
  browserWindow.index = 1;
  browser.activate();
  return true;
}

// eslint-disable-next-line @typescript-eslint/no-unused-vars -- osascript invokes this entry point.
function run(argv) {
  ObjC.import("AppKit");
  var target = workspaceLocation(argv[0]);
  var browsers = {
    "com.apple.Safari": "safari",
    "com.apple.SafariTechnologyPreview": "safari",
    "com.google.Chrome": "chromium",
    "org.chromium.Chromium": "chromium",
    "com.microsoft.edgemac": "chromium",
    "com.brave.Browser": "chromium",
    "com.vivaldi.Vivaldi": "chromium",
    "com.operasoftware.Opera": "chromium"
  };
  var processes = $.NSWorkspace.sharedWorkspace.runningApplications;
  var candidates = [];
  for (var p = 0; p < processes.count; p++) {
    var process = processes.objectAtIndex(p);
    var id = ObjC.unwrap(process.bundleIdentifier);
    if (browsers[id]) candidates.push({ id: id, active: process.active });
  }
  // Prefer the frontmost browser if several browsers have this workspace open.
  candidates.sort(function (a, b) { return Number(b.active) - Number(a.active); });
  for (var c = 0; c < candidates.length; c++) {
    var candidate = candidates[c];
    try {
      var browser = Application(candidate.id);
      var windows = browser.windows();
      var kind = browsers[candidate.id];
      // Prefer the selected document when several workspace tabs are open.
      for (var w = 0; w < windows.length; w++) {
        var browserWindow = windows[w];
        var selectedIndex = kind === "safari" ? 0 : browserWindow.activeTabIndex();
        var selected = kind === "safari" ? browserWindow.currentTab() : browserWindow.tabs()[selectedIndex - 1];
        if (activateTab(browser, browserWindow, selected, selectedIndex, kind, target)) return "activated";
      }
      for (var other = 0; other < windows.length; other++) {
        var otherWindow = windows[other];
        var tabs = otherWindow.tabs();
        for (var t = 0; t < tabs.length; t++) {
          if (activateTab(browser, otherWindow, tabs[t], t + 1, kind, target)) return "activated";
        }
      }
    // eslint-disable-next-line @typescript-eslint/no-unused-vars -- Automation errors trigger the OS fallback.
    } catch (error) {
      // Unsupported scripting or denied Automation permission: use the OS opener.
    }
  }
  return "not-found";
}
