// The Hearsay window. Calls main.API (gui_api.go) through the Wails runtime; opened in a
// plain browser (to work on the design) it uses mock.js instead.

let Call, Events, Browser;
try {
  ({ Call, Events, Browser } = await import("/wails/runtime.js"));
} catch {
  ({ Call, Events, Browser } = await import("./mock.js"));
}

const api = (method, ...args) => Call.ByName("main.API." + method, ...args);
const $ = (id) => document.getElementById(id);
const errText = (e) => {
  let m = (e && (e.message || e)) + "";
  try { m = JSON.parse(m).message || m; } catch {}
  return m.charAt(0).toUpperCase() + m.slice(1);
};

let state = null;

// ---------- helpers ----------

function keycaps(spec, os) {
  const names = {
    ctrl: os === "darwin" ? "⌃ Control" : "Ctrl", control: "Ctrl", alt: os === "darwin" ? "⌥ Option" : "Alt",
    option: "⌥ Option", shift: "Shift", super: os === "windows" ? "Win" : "Super", win: "Win",
    cmd: "⌘ Command", command: "⌘ Command", space: "Space", return: "Enter", enter: "Enter",
    escape: "Esc", esc: "Esc", tab: "Tab",
  };
  const wrap = document.createElement("span");
  wrap.className = "keys";
  for (const p of spec.split("+")) {
    const k = document.createElement("kbd");
    k.className = "key";
    k.textContent = names[p] || p.toUpperCase();
    wrap.append(k);
  }
  return wrap;
}

function say(el, text, tone) {
  el.textContent = text || "";
  if (tone) el.dataset.tone = tone; else delete el.dataset.tone;
}

// busy runs fn with btn showing a spinner.
async function busy(btn, fn) {
  btn.classList.add("busy");
  btn.disabled = true;
  try { return await fn(); } finally { btn.classList.remove("busy"); btn.disabled = false; }
}

// ---------- state ----------

async function refresh() {
  state = await api("State");
  document.body.classList.toggle("mac", state.os === "darwin");
  for (const el of document.querySelectorAll(".trayname")) {
    el.textContent = state.os === "darwin" ? "menu bar icon" : "tray icon";
  }
  $("version").textContent = "Version " + state.version;
  renderStatus(state.status);
  renderFacts();
  renderSettings();
  $("tr-config").textContent = state.configPath;
  return state;
}

function renderStatus(st) {
  const hero = $("hero");
  if (!state?.phone) {
    hero.dataset.state = "unpaired";
    $("st-label").textContent = "Not set up";
    $("st-sub").textContent = "Pair your phone to start dictating.";
    $("st-toggle").textContent = "Set up";
    $("st-cancel").hidden = true;
    $("st-error").hidden = true;
    return;
  }
  hero.dataset.state = st.state;
  $("st-label").textContent = st.label;
  const sub = $("st-sub");
  sub.textContent = "";
  if (st.state === "listening") {
    sub.textContent = "Talk to your phone. Press again to paste.";
  } else if (st.state === "busy") {
    sub.textContent = "Talking to the phone…";
  } else if (st.state === "retry") {
    sub.textContent = "The phone may still be listening. Press again to finish.";
  } else if (state.hotkeyName && !state.hotkeyError) {
    sub.append("Press ", keycaps(state.hotkey, state.os), " anywhere to dictate.");
  } else {
    sub.textContent = "Use the button below or the tray menu to dictate.";
  }
  const toggle = $("st-toggle");
  toggle.disabled = st.state === "busy";
  toggle.textContent = { listening: "Stop and paste", retry: "Finish", busy: "Working…" }[st.state] || "Start dictation";
  $("st-cancel").hidden = st.state !== "listening";
  $("st-error").hidden = !st.lastError || st.state === "busy" || st.state === "listening";
  $("st-error-text").textContent = st.lastError;
}

function renderFacts() {
  const s = state;
  $("st-phone").textContent = s.phone ? s.phone.name.replace(/^Hearsay on /, "") : "No phone paired";
  $("st-addr").textContent = s.phone ? s.phone.addr : "";
  $("st-check").hidden = !s.phone;
  $("st-conn").hidden = !s.phone;
  const hk = $("st-hotkey");
  hk.textContent = "";
  if (s.hotkey === "none") hk.textContent = "Off";
  else hk.append(keycaps(s.hotkey, s.os));
  $("st-hotkey-line").textContent = s.wayland
    ? (s.desktopShortcuts ? "A desktop shortcut runs Hearsay (Wayland has no app hotkeys)." : "Set as a shortcut in your desktop's settings.")
    : (s.hotkeyError ? "Not working" : "Works in any app.");
  const he = $("st-hotkey-error");
  he.hidden = !s.hotkeyError;
  he.textContent = s.hotkeyError;
  $("st-keyboard").textContent = s.keyboard || "Not chosen";
  $("st-nokeyboard").hidden = !s.phone || !!s.keyboardId;
  $("st-paste").textContent = s.paste ? "Pasted where you're typing (and copied)" : "Copied to the clipboard only";
}

// ---------- navigation ----------

function showPage(name) {
  for (const b of document.querySelectorAll(".nav")) {
    if (b.dataset.page === name) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
  }
  for (const p of document.querySelectorAll(".page")) p.hidden = p.id !== "page-" + name;
  if (name !== "settings") stopRecording();
}
for (const b of document.querySelectorAll(".nav")) b.onclick = () => showPage(b.dataset.page);

function showMain() {
  $("wizard").hidden = true;
  $("main").hidden = false;
  showPage("status");
}

// ---------- status page ----------

$("st-toggle").onclick = () => {
  if (!state.phone) return openWizard("install", false);
  api("Toggle");
};
$("st-cancel").onclick = () => api("Cancel");
$("st-pick-keyboard").onclick = () => openWizard("keyboard", true);

async function checkConnection() {
  const pill = $("st-conn");
  say(pill, "Checking…", "busy");
  try {
    await api("CheckConnection");
    say(pill, "Connected", "ok");
  } catch (e) {
    say(pill, "Not reachable", "bad");
    pill.title = errText(e);
    return errText(e);
  }
}
$("st-check").onclick = checkConnection;

// ---------- settings ----------

function renderSettings() {
  const s = state;
  if (!recording) {
    const r = $("set-hotkey");
    r.textContent = "";
    if (s.hotkey === "none") r.textContent = "Off: click to set one";
    else r.append(keycaps(s.hotkey, s.os));
  }
  $("set-hotkey-none").hidden = s.hotkey === "none";
  const recorderUsable = !s.wayland || s.desktopShortcuts;
  $("set-hotkey").disabled = !recorderUsable;
  $("set-hotkey-help").textContent = s.wayland && !s.desktopShortcuts
    ? "On Wayland, apps can't set hotkeys. Add a shortcut in your desktop's keyboard settings that runs: " + s.toggleCommand
    : "Press it once to start listening, again to paste what you said.";
  $("set-paste").checked = s.paste;
  $("set-paste").disabled = s.pasteForcedOff;
  $("set-login").checked = s.autostart;
  if (s.autostartOther && !s.autostart) {
    say($("set-login-msg"), "Another copy of Hearsay starts at login. Turn this on to start this one instead.");
  }
}

let recording = false;
const modOrder = ["ctrl", "alt", "shift", "super"];

function modName(m) {
  if (m === "alt") return state.os === "darwin" ? "option" : "alt";
  if (m === "super") return state.os === "darwin" ? "cmd" : state.os === "windows" ? "win" : "super";
  return m;
}

function keyName(e) {
  const c = e.code;
  if (/^Key[A-Z]$/.test(c)) return c.slice(3).toLowerCase();
  if (/^F([1-9]|1[0-2])$/.test(c)) return c.toLowerCase();
  return { Space: "space", Enter: "return", Tab: "tab", Escape: "escape" }[c];
}

function startRecording() {
  recording = true;
  const r = $("set-hotkey");
  r.classList.add("recording");
  r.textContent = "Press the new keys…";
  say($("set-hotkey-msg"), "Hold the modifiers, then press a letter, Space or an F key. Esc cancels.");
  api("PauseHotkey", true);
}

function stopRecording() {
  if (!recording) return;
  recording = false;
  $("set-hotkey").classList.remove("recording");
  api("PauseHotkey", false);
  if (state) renderSettings();
}

$("set-hotkey").onclick = () => (recording ? stopRecording() : startRecording());
$("set-hotkey").addEventListener("blur", () => { stopRecording(); });

document.addEventListener("keydown", async (e) => {
  if (!recording) return;
  e.preventDefault();
  const mods = [];
  if (e.ctrlKey) mods.push("ctrl");
  if (e.altKey) mods.push("alt");
  if (e.shiftKey) mods.push("shift");
  if (e.metaKey) mods.push("super");
  if (e.code === "Escape" && mods.length === 0) {
    stopRecording();
    say($("set-hotkey-msg"), "");
    return;
  }
  const key = keyName(e);
  if (!key) {
    const r = $("set-hotkey");
    r.textContent = "";
    if (mods.length) r.append(keycaps(mods.map(modName).join("+") + "+…", state.os));
    return;
  }
  if (mods.length === 0 && !key.startsWith("f")) {
    say($("set-hotkey-msg"), "Add Ctrl, Alt, Shift or another modifier, so normal typing doesn't trigger it.", "bad");
    return;
  }
  const spec = mods.sort((a, b) => modOrder.indexOf(a) - modOrder.indexOf(b)).map(modName).concat(key).join("+");
  recording = false;
  $("set-hotkey").classList.remove("recording");
  await setHotkey(spec);
});

async function setHotkey(spec) {
  const msg = $("set-hotkey-msg");
  try {
    await api("SetHotkey", spec);
    await refresh();
    say(msg, spec === "none" ? "Hotkey turned off. Dictate from the tray menu." : "Saved. It works right away.", "ok");
  } catch (e) {
    await api("PauseHotkey", false);
    await refresh();
    say(msg, errText(e), "bad");
  }
}
$("set-hotkey-none").onclick = () => setHotkey("none");

$("set-paste").onchange = async (e) => {
  try {
    await api("SetPaste", e.target.checked);
    await refresh();
    say($("set-paste-msg"), "");
  } catch (err) {
    e.target.checked = !e.target.checked;
    say($("set-paste-msg"), errText(err), "bad");
  }
};

$("set-login").onchange = async (e) => {
  try {
    await api("SetAutostart", e.target.checked);
    await refresh();
    say($("set-login-msg"), "");
  } catch (err) {
    e.target.checked = !e.target.checked;
    say($("set-login-msg"), errText(err), "bad");
  }
};

// ---------- troubleshoot ----------

$("tr-conn").onclick = (e) => busy(e.currentTarget, async () => {
  const msg = $("tr-conn-msg");
  try {
    say(msg, await api("CheckConnection"), "ok");
    say($("st-conn"), "Connected", "ok");
  } catch (err) {
    say(msg, errText(err), "bad");
    say($("st-conn"), "Not reachable", "bad");
  }
});
$("tr-pair").onclick = () => openWizard("presser", true);
$("tr-forget").onclick = async (e) => {
  const btn = e.currentTarget;
  const msg = $("tr-forget-msg");
  if (!btn.dataset.confirm) {
    btn.dataset.confirm = "1";
    btn.textContent = "Forget it";
    say(msg, "Click again to unpair. You'll need the phone to pair again.");
    setTimeout(() => { delete btn.dataset.confirm; btn.textContent = "Forget phone"; }, 5000);
    return;
  }
  delete btn.dataset.confirm;
  btn.textContent = "Forget phone";
  await busy(btn, async () => {
    try { say(msg, await api("Forget"), "ok"); } catch (err) { say(msg, errText(err), "bad"); }
    await refresh();
  });
};
$("tr-log").onclick = () => api("OpenLog");
$("tr-report").onclick = (e) => busy(e.currentTarget, async () => {
  const msg = $("tr-report-msg");
  try {
    const p = await api("SaveReport");
    say(msg, p ? "Saved to " + p : "", "ok");
  } catch (err) {
    say(msg, errText(err), "bad");
  }
});

// ---------- wizard ----------

const order = ["install", "presser", "pair", "keyboard", "test", "finish"];
let step = null;
let fromMain = false; // opened from the main window (pair again), not first run
let pairWith = null;  // {name, addr}

function openWizard(first, fromMainWindow) {
  fromMain = fromMainWindow;
  $("main").hidden = true;
  $("wizard").hidden = false;
  $("wz-close").hidden = !fromMain;
  go(first);
}

function go(name, back = false) {
  leave(step);
  step = name;
  const idx = order.indexOf(name);
  for (const li of document.querySelectorAll("#steps li")) {
    const i = order.indexOf(li.dataset.step);
    li.classList.toggle("current", i === idx);
    li.classList.toggle("done", i < idx);
    if (i === idx) li.setAttribute("aria-current", "step"); else li.removeAttribute("aria-current");
  }
  for (const s of document.querySelectorAll(".step")) {
    const on = s.dataset.step === name;
    s.hidden = !on;
    if (on) {
      s.classList.remove("enter", "back");
      void s.offsetWidth;
      s.classList.add("enter");
      s.classList.toggle("back", back);
    }
  }
  enter(name, back);
}

function next() { go(order[order.indexOf(step) + 1]); }
function back() { go(order[order.indexOf(step) - 1], true); }
for (const b of document.querySelectorAll("[data-next]")) b.onclick = next;
for (const b of document.querySelectorAll("[data-back]")) b.onclick = back;
$("wz-close").onclick = () => { leave(step); step = null; refresh().then(showMain); };

function enter(name, back) {
  ({ install: enterInstall, presser: enterPresser, pair: enterPair, keyboard: enterKeyboard, test: enterTest, finish: enterFinish })[name](back);
}
function leave(name) {
  if (name === "presser") searching = false;
  if (name === "pair") api("PairCancel");
}

// 1. install
let qrDrawn = false;
async function enterInstall() {
  $("wz-release").textContent = "the download link";
  if (qrDrawn) return;
  const q = await api("InstallQR");
  $("qr").innerHTML = `<svg viewBox="0 0 ${q.size} ${q.size}" shape-rendering="crispEdges"><path fill="#15161a" d="${q.path}"/></svg>`;
  qrDrawn = true;
}
$("wz-release").onclick = () => Browser.OpenURL(state.releaseUrl);

// 2. key presser: look for the phone until it shows up. The first time, one phone found
// goes straight on to pairing; coming back (or pairing again), the user picks it.
let searching = false;
async function enterPresser(back) {
  const auto = !back && !fromMain;
  const box = $("wz-search");
  const list = $("wz-phones");
  list.textContent = "";
  box.dataset.state = "looking";
  say($("wz-search-text"), "Looking for your phone on the network…");
  say($("wz-addr-msg"), "");
  searching = true;
  while (searching && step === "presser") {
    const found = await api("FindPhones", 3);
    if (!searching || step !== "presser") return;
    if (found.length === 1 && auto) {
      box.dataset.state = "found";
      say($("wz-search-text"), `Found ${found[0].name.replace(/^Hearsay on /, "")}. The key presser is on.`);
      searching = false;
      pairWith = found[0];
      setTimeout(() => step === "presser" && next(), 900);
      return;
    }
    if (found.length > 0) {
      box.dataset.state = "found";
      say($("wz-search-text"), found.length === 1 ? "Found this phone. Pick it to pair:" : "Found several phones. Pick yours:");
      list.textContent = "";
      for (const p of found) {
        const li = document.createElement("li");
        const b = document.createElement("button");
        b.className = "phone";
        b.innerHTML = "<b></b><span></span>";
        b.querySelector("b").textContent = p.name.replace(/^Hearsay on /, "");
        b.querySelector("span").textContent = p.addr;
        b.onclick = () => { searching = false; pairWith = p; next(); };
        li.append(b);
        list.append(li);
      }
    }
  }
}
$("wz-addr-form").onsubmit = async (e) => {
  e.preventDefault();
  const btn = e.submitter || e.target.querySelector("button");
  await busy(btn, async () => {
    try {
      pairWith = await api("CheckAddress", $("wz-addr").value);
      searching = false;
      next();
    } catch (err) {
      say($("wz-addr-msg"), errText(err), "bad");
    }
  });
};

// 3. pair
async function enterPair() {
  const box = $("wz-code-box");
  const code = $("wz-code");
  const help = $("wz-code-help");
  const msg = $("wz-pair-msg");
  $("wz-pair-retry").hidden = true;
  say(msg, "");
  if (!pairWith) return go("presser", true);
  $("wz-pair-name").textContent = pairWith.name.replace(/^Hearsay on /, "");
  box.dataset.state = "connecting";
  code.textContent = "––– –––";
  help.textContent = "Connecting…";
  const mine = step;
  try {
    const c = await api("PairStart", pairWith);
    if (step !== mine) return;
    box.dataset.state = "code";
    code.textContent = c;
    help.textContent = "Check that the phone shows the same code, then tap Pair on the phone. If it differs, tap Cancel there.";
    await api("PairFinish");
    if (step !== mine) return;
    box.dataset.state = "paired";
    help.textContent = "Paired.";
    await refresh();
    setTimeout(() => step === "pair" && next(), 700);
  } catch (e) {
    if (step !== mine || errText(e) === "Cancelled") return;
    box.dataset.state = "failed";
    code.textContent = "––– –––";
    help.textContent = "";
    say(msg, errText(e), "bad");
    $("wz-pair-retry").hidden = false;
  }
}
$("wz-pair-retry").onclick = () => enterPair();
const off = Events.On("pair:waiting", () => {
  if (step !== "pair") return;
  $("wz-code-box").dataset.state = "waiting";
  $("wz-code-help").textContent = "Waiting for the Hearsay app to be on screen on the phone…";
});
void off;

// 4. keyboard
let kbList = [];
let kbChosen = null;
async function enterKeyboard() {
  const msg = $("wz-kb-msg");
  say(msg, "Asking the phone for its keyboards…");
  $("wz-kbs").textContent = "";
  $("wz-kb-checks").hidden = true;
  $("wz-kb-next").disabled = true;
  try {
    kbList = await api("Keyboards");
  } catch (e) {
    say(msg, errText(e), "bad");
    return;
  }
  say(msg, "");
  kbChosen = (kbList.find((k) => k.selected) || (kbList.length === 1 ? kbList[0] : null))?.id ?? kbChosen;
  renderKeyboards();
}
function renderKeyboards() {
  const box = $("wz-kbs");
  box.textContent = "";
  for (const k of kbList) {
    const b = document.createElement("button");
    b.className = "kb";
    b.setAttribute("role", "radio");
    b.setAttribute("aria-checked", String(k.id === kbChosen));
    b.innerHTML = "<span></span><small></small>";
    b.querySelector("span").textContent = k.label;
    b.querySelector("small").textContent = k.id.split("/")[0];
    b.onclick = () => { kbChosen = k.id; renderKeyboards(); };
    box.append(b);
  }
  const k = kbList.find((x) => x.id === kbChosen);
  $("wz-kb-checks").hidden = !k;
  if (!k) return;
  const en = $("wz-kb-enabled");
  en.className = "check" + (k.enabled ? "" : " bad");
  en.textContent = k.enabled ? `${k.label} is turned on as a keyboard.`
    : `${k.label} isn't turned on. On the phone: Settings > System > Keyboard > On-screen keyboard, turn on ${k.label}.`;
  const mic = $("wz-kb-mic");
  mic.className = "check" + (k.mic ? "" : " bad");
  mic.textContent = k.mic ? `${k.label} may use the microphone.`
    : `${k.label} can't use the microphone. On the phone: Settings > Apps > ${k.label} > Permissions > Microphone, allow it.`;
  $("wz-kb-next").disabled = !(k.enabled && k.mic);
}
$("wz-kb-recheck").onclick = (e) => busy(e.currentTarget, enterKeyboard);
$("wz-kb-next").onclick = async (e) => {
  await busy(e.currentTarget, async () => {
    try {
      await api("SetKeyboard", kbChosen);
      await refresh();
      if (fromMain && state.phone) { $("wz-close").click(); return; }
      next();
    } catch (err) {
      say($("wz-kb-msg"), errText(err), "bad");
    }
  });
};

// 5. test
let trying = "ready";
function enterTest() {
  setTry("ready", "Press Start, then talk.");
  $("wz-heard").hidden = true;
  $("wz-test-next").textContent = "Skip";
}
function setTry(st, help) {
  trying = st;
  $("wz-try").dataset.state = st;
  $("wz-try-label").textContent = { ready: "Start", listening: "Stop", working: "…" }[st];
  $("wz-try-help").textContent = help;
}
$("wz-try-btn").onclick = async () => {
  if (trying === "ready") {
    setTry("working", "Starting Rambler on the phone…");
    try {
      await api("TestStart");
      setTry("listening", "Listening. Say a sentence, then press Stop.");
    } catch (e) {
      setTry("ready", errText(e));
    }
  } else if (trying === "listening") {
    setTry("working", "Getting the text…");
    try {
      const text = await api("TestStop");
      const q = $("wz-heard");
      if (text.trim()) {
        q.textContent = text;
        q.hidden = false;
        setTry("ready", "That's what Rambler heard. It's on the clipboard too.");
        $("wz-test-next").textContent = "Continue";
        $("wz-test-next").classList.add("primary");
      } else {
        q.hidden = true;
        setTry("ready", "Rambler heard nothing. Check that Rambler (not regular voice typing) is chosen in Gboard, then try again.");
      }
    } catch (e) {
      setTry("ready", errText(e));
    }
  }
};

// 6. finish
function enterFinish() {
  const s = state;
  const keys = $("wz-finish-keys");
  keys.textContent = "";
  const lede = $("wz-finish-lede");
  const way = $("wz-finish-wayland");
  way.hidden = true;
  if (s.hotkey === "none") {
    lede.textContent = `Dictate from the ${s.os === "darwin" ? "menu bar icon" : "tray icon"}: Start dictation, then Stop and paste.`;
  } else {
    lede.textContent = "Wherever you're typing, press this to start listening, and again to paste what you said:";
    keys.append(...keycaps(s.hotkey, s.os).children);
    if (s.wayland && !s.desktopShortcuts) {
      way.hidden = false;
      way.textContent = "Wayland doesn't let apps set hotkeys: add a shortcut in your desktop's keyboard settings that runs " + s.toggleCommand;
    }
  }
  $("wz-login").checked = s.autostart || !s.autostartOther;
  say($("wz-finish-msg"), "");
}
$("wz-done").onclick = (e) => busy(e.currentTarget, async () => {
  const msg = $("wz-finish-msg");
  try {
    await api("SetAutostart", $("wz-login").checked);
  } catch (err) {
    if ($("wz-login").checked) say(msg, errText(err), "bad");
  }
  try {
    await api("FinishSetup");
  } catch (err) {
    say(msg, errText(err), "bad");
    return;
  }
  step = null;
  await refresh();
  showMain();
});

// ---------- start ----------

Events.On("status", (ev) => {
  if (!state) return;
  state.status = ev.data;
  renderStatus(ev.data);
});
Events.On("state", () => { if (state) refresh(); });

await refresh();
if (state.phone) {
  showMain();
  checkConnection();
} else {
  openWizard("install", false);
}
