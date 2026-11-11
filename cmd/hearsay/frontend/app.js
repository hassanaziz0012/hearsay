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
  $("version").textContent = "Version " + state.version + (state.update ? " · update available" : "");
  renderUpdate();
  renderPhoneApp();
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

function renderUpdate() {
  const u = state.update;
  $("st-update").hidden = !u;
  if (!u) return;
  $("st-update-title").textContent = "Hearsay " + u.version + " is out.";
  $("st-update-text").textContent = u.canInstall
    ? "You have " + state.version + ". Updating takes a few seconds and restarts Hearsay."
    : u.managed
    ? "You have " + state.version + ". Update it with your package manager."
    : "You have " + state.version + ". Download the new version from the release page.";
  $("st-update-install").hidden = !u.canInstall;
  $("st-update-notes").textContent = u.canInstall ? "What's new" : "Open the release page";
}

$("st-update-notes").onclick = () => Browser.OpenURL(state.update.page);
$("st-update-install").onclick = (e) => busy(e.currentTarget, async () => {
  const msg = $("st-update-msg");
  say(msg, "Downloading…");
  try {
    await api("InstallUpdate");
    say(msg, "Updated. Restarting…", "ok");
  } catch (err) {
    say(msg, errText(err), "bad");
  }
});

// The phone app is older (or newer) than the one released with this program.
let phoneQrDrawn = false;
async function renderPhoneApp() {
  const p = state.phoneApp;
  const box = $("st-phoneapp");
  box.hidden = !p || !(p.outdated || p.newer);
  if (box.hidden) return;
  const have = p.version ? " (it has " + p.version + ")" : "";
  if (p.outdated) {
    $("st-phoneapp-title").textContent = "Update the Hearsay app on your phone" + have + ".";
    $("st-phoneapp-text").textContent = "Open Hearsay on the phone and tap Download, or scan this code with the phone. Then open the downloaded file and tap Update.";
  } else {
    $("st-phoneapp-title").textContent = "The phone app is newer than Hearsay on this computer.";
    $("st-phoneapp-text").textContent = "Update Hearsay here: Settings, Check for updates.";
  }
  $("st-phoneapp-link").hidden = !p.outdated;
  $("st-phoneapp-qr").hidden = !p.outdated;
  if (p.outdated && !phoneQrDrawn) {
    const q = await api("InstallQR");
    $("st-phoneapp-qr").innerHTML = `<svg viewBox="0 0 ${q.size} ${q.size}" shape-rendering="crispEdges"><path fill="#050f16" d="${q.path}"/></svg>`;
    phoneQrDrawn = true;
  }
}
$("st-phoneapp-link").onclick = () => Browser.OpenURL(state.releaseUrl);

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
  if (name === "stats") loadStats(); else closeTyping();
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

// ---------- stats ----------

let stats = null;
let range = "week";
const num = (n) => n.toLocaleString();
const plural = (n, one, many) => `${num(n)} ${n === 1 ? one : many || one + "s"}`;
const dayOf = (date) => new Date(date + "T12:00:00");
const dayText = (date, opts) => dayOf(date).toLocaleDateString(undefined, opts || { weekday: "short", day: "numeric", month: "short" });
const weekdays = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

function duration(ms) {
  const min = Math.round(ms / 60000);
  if (ms <= 0) return "0 min";
  if (min < 1) return "< 1 min";
  if (min < 60) return min + " min";
  return `${Math.floor(min / 60)} h ${min % 60} min`;
}

function hourText(h) {
  return new Date(2000, 0, 1, h).toLocaleTimeString(undefined, { hour: "numeric" });
}

async function loadStats() {
  try {
    stats = await api("Stats");
    say($("stats-error"), "");
  } catch (e) {
    say($("stats-error"), errText(e), "bad");
    return;
  }
  renderStats();
}

// bars draws one column per item ({value, label, tip}); the line above it says what the
// pointer is on, and says rest otherwise.
function bars(el, readout, items, rest) {
  const max = Math.max(1, ...items.map((it) => it.value));
  el.textContent = "";
  readout.textContent = rest;
  for (const it of items) {
    const col = document.createElement("div");
    col.className = "bar";
    col.tabIndex = 0;
    col.setAttribute("aria-label", it.tip);
    const mark = document.createElement("i");
    mark.style.height = (it.value / max) * 100 + "%";
    if (!it.value) mark.className = "none";
    const label = document.createElement("span");
    label.textContent = it.label || "";
    col.append(mark, label);
    col.onpointerenter = col.onfocus = () => { readout.textContent = it.tip; };
    col.onpointerleave = col.onblur = () => { readout.textContent = rest; };
    el.append(col);
  }
}

function renderWords() {
  const days = stats.days;
  const tip = (d) => `${dayText(d.date)}: ${plural(d.words, "word")} in ${plural(d.dictations, "dictation")}`;
  let items, rest;
  if (range === "year") {
    const months = new Map();
    for (const d of days) {
      const key = d.date.slice(0, 7);
      const m = months.get(key) || { date: d.date, words: 0, dictations: 0 };
      m.words += d.words;
      m.dictations += d.dictations;
      months.set(key, m);
    }
    const last = [...months.values()].slice(-12);
    items = last.map((m) => ({
      value: m.words,
      label: dayText(m.date, { month: "short" }),
      tip: `${dayText(m.date, { month: "long", year: "numeric" })}: ${plural(m.words, "word")} in ${plural(m.dictations, "dictation")}`,
    }));
    const total = last.reduce((n, m) => n + m.words, 0);
    rest = `${plural(total, "word")} in the last 12 months, ${num(Math.round(total / 12))} a month on average`;
  } else {
    const n = range === "week" ? 7 : 30;
    const last = days.slice(-n);
    items = last.map((d) => ({
      value: d.words,
      label: n === 7 ? dayText(d.date, { weekday: "short" })
        : dayOf(d.date).getDay() === 1 ? dayText(d.date, { day: "numeric", month: "short" }) : "",
      tip: tip(d),
    }));
    const total = last.reduce((sum, d) => sum + d.words, 0);
    rest = `${plural(total, "word")} in the last ${n} days, ${num(Math.round(total / n))} a day on average`;
  }
  bars($("stats-words"), $("stats-words-read"), items, rest);
}

// The year as a grid: a column per week (Monday on top), a square per day, darker for
// more words.
function renderHeat() {
  const days = stats.days;
  const max = Math.max(1, ...days.map((d) => d.words));
  const active = days.filter((d) => d.dictations > 0).length;
  const rest = `You dictated on ${plural(active, "day")} of the last ${num(days.length)}`;
  const readout = $("stats-heat-read");
  readout.textContent = rest;
  const heat = $("stats-heat");
  heat.textContent = "";
  const months = $("stats-heat-months");
  months.textContent = "";
  const weeks = Math.ceil(days.length / 7);
  months.style.gridTemplateColumns = `repeat(${weeks}, 1fr)`;
  let lastLabel = -4;
  days.forEach((d, i) => {
    const cell = document.createElement("i");
    if (d.words) cell.dataset.l = Math.max(1, Math.ceil((4 * d.words) / max));
    const tip = `${dayText(d.date, { weekday: "short", day: "numeric", month: "short", year: "numeric" })}: `
      + (d.dictations ? `${plural(d.words, "word")} in ${plural(d.dictations, "dictation")}` : "nothing");
    cell.setAttribute("aria-label", tip);
    cell.onpointerenter = () => { readout.textContent = tip; };
    cell.onpointerleave = () => { readout.textContent = rest; };
    heat.append(cell);
    // Name a month over the week its first day falls in, unless that crowds the last name.
    const week = Math.floor(i / 7);
    if (d.date.endsWith("-01") && week - lastLabel >= 3) {
      const m = document.createElement("span");
      m.textContent = dayText(d.date, { month: "short" });
      m.style.gridColumn = week + 1;
      months.append(m);
      lastLabel = week;
    }
  });
}

function renderStats() {
  const s = stats;
  const none = s.total.dictations === 0;
  $("stats-empty").hidden = !none;
  $("stats-empty-text").textContent = state.statsOn
    ? "Nothing to show yet. Your next dictation starts the count. Hearsay keeps numbers only (when, how many words, how long), never what you said."
    : "Nothing to show: stats are off. Turn on Keep stats below to count your dictations.";
  $("stats-off").hidden = state.statsOn || none;
  $("stats-on").checked = state.statsOn;
  $("stats-body").hidden = none;
  // Cancelled and empty dictations are records too, so there may be some to export or delete.
  $("stats-export-row").hidden = $("stats-reset-row").hidden = !s.since;
  $("stats-since").textContent = s.since ? `Counting since ${dayText(s.since, { day: "numeric", month: "long", year: "numeric" })}.` : "";
  if (none) return;

  for (const k of ["today", "week", "month", "total"]) {
    $("stats-" + k).textContent = num(s[k].words);
    $("stats-" + k + "-n").textContent = `${s[k].words === 1 ? "word" : "words"} in ${plural(s[k].dictations, "dictation")}`;
  }
  // This week against last week up to the same weekday (the days start on a Monday).
  const sofar = s.days.length % 7 || 7;
  const before = s.days.slice(-sofar - 7, -7).reduce((n, d) => n + d.words, 0);
  const change = before ? Math.round(((s.week.words - before) / before) * 100) : 0;
  $("stats-week-cmp").textContent = !before ? (s.week.words ? "nothing by this day last week" : "")
    : change === 0 ? "level with last week by this day"
    : `${change > 0 ? "▲" : "▼"} ${num(Math.abs(change))}% on last week by this day (${num(before)})`;
  renderWords();
  renderHeat();

  const topHour = s.byHour.indexOf(Math.max(...s.byHour));
  bars($("stats-hours"), $("stats-hours-read"), s.byHour.map((w, h) => ({
    value: w,
    label: h % 6 === 0 ? hourText(h) : "",
    tip: `${hourText(h)} to ${hourText((h + 1) % 24)}: ${plural(w, "word")}`,
  })), `Most words around ${hourText(topHour)}`);
  const topDay = s.byWeekday.indexOf(Math.max(...s.byWeekday));
  bars($("stats-weekdays"), $("stats-weekdays-read"), s.byWeekday.map((w, i) => ({
    value: w,
    label: weekdays[i].slice(0, 2),
    tip: `${weekdays[i]}s: ${plural(w, "word")}`,
  })), `Most words on ${weekdays[topDay]}s`);

  const timed = s.speakMs > 0;
  $("stats-pace").textContent = timed ? num(s.paceWpm) : "–";
  $("stats-typing").textContent = num(s.typingWpm);
  $("stats-typing-note").textContent = s.typingMeasured ? "words a minute." : "words a minute is a guess.";
  $("stats-typing-open").textContent = s.typingMeasured ? "Measure again" : "Measure yours";
  $("stats-spoke").textContent = timed ? duration(s.speakMs) : "–";
  $("stats-spoke-note").textContent = timed ? "from start to stop" : "counted from your next dictation";
  $("stats-saved").textContent = timed ? duration(s.savedMs) : "–";
  $("stats-saved-note").textContent = !timed ? ""
    : s.savedMs > 0 ? `against typing at ${s.typingWpm} words a minute`
    : "you type at least as fast as you talk";

  const rec = $("stats-records");
  rec.textContent = "";
  const row = (name, value, note) => {
    const div = document.createElement("div");
    const dt = document.createElement("dt");
    dt.textContent = name;
    const dd = document.createElement("dd");
    dd.textContent = value;
    if (note) {
      const small = document.createElement("span");
      small.textContent = note;
      dd.append(" ", small);
    }
    div.append(dt, dd);
    rec.append(div);
  };
  row("Current streak", plural(s.streak, "day"), s.streak > 0 && s.today.dictations === 0 ? "dictate today to keep it" : "");
  row("Longest streak", plural(s.longestStreak, "day"));
  if (s.bestDay) row("Best day", plural(s.bestDay.words, "word"), dayText(s.bestDay.date, { day: "numeric", month: "short", year: "numeric" }));
  row("Longest dictation", plural(s.longestWords, "word"), dayText(s.longestDate, { day: "numeric", month: "short", year: "numeric" }));
  row("Average dictation", plural(Math.round(s.total.words / s.total.dictations), "word"));
  const activeDays = s.days.filter((d) => d.dictations > 0).length;
  if (activeDays) row("On a day you dictate", plural(Math.round(s.days.reduce((n, d) => n + d.words, 0) / activeDays), "word"), "average over the last year");
  row("Cancelled", plural(s.cancelled, "dictation"));
  row("Nothing heard", plural(s.empty, "time"));
}

for (const b of document.querySelectorAll("#stats-range button")) {
  b.onclick = () => {
    range = b.dataset.range;
    for (const o of document.querySelectorAll("#stats-range button")) o.setAttribute("aria-pressed", String(o === b));
    renderWords();
  };
}

$("stats-on").onchange = async (e) => {
  try {
    await api("SetStats", e.target.checked);
    await refresh();
    say($("stats-on-msg"), "");
  } catch (err) {
    e.target.checked = !e.target.checked;
    say($("stats-on-msg"), errText(err), "bad");
  }
  renderStats();
};

$("stats-export").onclick = (e) => busy(e.currentTarget, async () => {
  const msg = $("stats-export-msg");
  try {
    const p = await api("ExportStats");
    say(msg, p ? "Saved to " + p : "", "ok");
  } catch (err) {
    say(msg, errText(err), "bad");
  }
});

$("stats-reset").onclick = async (e) => {
  const btn = e.currentTarget;
  const msg = $("stats-reset-msg");
  if (!btn.dataset.confirm) {
    btn.dataset.confirm = "1";
    btn.textContent = "Delete them";
    say(msg, "Click again to delete every stat. This can't be undone.");
    setTimeout(() => { delete btn.dataset.confirm; btn.textContent = "Reset stats"; say(msg, ""); }, 5000);
    return;
  }
  delete btn.dataset.confirm;
  btn.textContent = "Reset stats";
  await busy(btn, async () => {
    try { await api("ResetStats"); say(msg, ""); } catch (err) { say(msg, errText(err), "bad"); }
    await loadStats();
  });
};

// The typing test: type a short text, and the speed it took becomes what dictating is
// compared with. Speed is the usual measure: five correct characters make a word.
const typingTexts = [
  "The kettle clicked off just as the rain started, so I made tea and sat by the window. Nothing on my list was urgent, and for once I let the afternoon go by without checking the time.",
  "Most of what we write in a day is small: a reply, a note, a line in a document. None of it is hard to say out loud, yet each one waits for our hands to catch up with what we already know.",
  "She packed the car before sunrise and left a note on the table. The road north was empty for the first hour, with fog in the low fields and a pale light coming up behind the hills.",
  "A good tool gets out of the way. You stop thinking about how it works and think about the job, and at the end of the day you remember what you made, not what you made it with.",
];
let typing = null; // {text, typed, started, done}

function openTyping() {
  $("typing").hidden = false;
  newTyping();
  $("typing").scrollIntoView({ block: "nearest", behavior: "smooth" });
}

function closeTyping() {
  typing = null;
  $("typing").hidden = true;
}

function newTyping() {
  let text;
  do { text = typingTexts[Math.floor(Math.random() * typingTexts.length)]; } while (typing && text === typing.text);
  typing = { text, typed: "", started: 0, done: false, wpm: 0 };
  $("typing-help").textContent = "Type the text below as you normally would. The clock starts with your first key and stops at the last one.";
  say($("typing-result"), "");
  $("typing-save").hidden = true;
  drawTyping();
  $("typing-text").focus({ preventScroll: true });
}

function drawTyping() {
  const box = $("typing-text");
  box.textContent = "";
  const { text, typed } = typing;
  [...text].forEach((ch, i) => {
    const s = document.createElement("span");
    s.textContent = ch;
    if (i < typed.length) s.className = typed[i] === ch ? "ok" : "bad";
    else if (i === typed.length && !typing.done) s.className = "cur";
    box.append(s);
  });
}

$("typing-text").addEventListener("keydown", (e) => {
  if (!typing || typing.done || e.ctrlKey || e.metaKey || e.altKey) return;
  if (e.key === "Backspace") {
    typing.typed = typing.typed.slice(0, -1);
  } else if (e.key.length === 1) {
    if (!typing.started) typing.started = performance.now();
    typing.typed += e.key;
  } else {
    return;
  }
  e.preventDefault(); // Space would scroll the page
  if (typing.typed.length >= typing.text.length) finishTyping();
  drawTyping();
});

function finishTyping() {
  const t = typing;
  t.done = true;
  const minutes = (performance.now() - t.started) / 60000;
  const right = [...t.text].filter((ch, i) => t.typed[i] === ch).length;
  const accuracy = right / t.text.length;
  t.wpm = Math.round(right / 5 / minutes);
  const res = $("typing-result");
  if (accuracy < 0.85 || t.wpm < 5 || t.wpm > 300) {
    say(res, "Too many slips for a fair reading. Try another text.", "bad");
    return;
  }
  say(res, `${t.wpm} words a minute, ${Math.round(accuracy * 100)}% accurate.`);
  $("typing-save").hidden = false;
  $("typing-save").focus({ preventScroll: true });
}

$("stats-typing-open").onclick = openTyping;
$("typing-close").onclick = closeTyping;
$("typing-again").onclick = newTyping;
$("typing-save").onclick = (e) => busy(e.currentTarget, async () => {
  try {
    await api("SetTypingSpeed", typing.wpm);
    closeTyping();
    await loadStats();
  } catch (err) {
    say($("typing-result"), errText(err), "bad");
  }
});

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
  $("set-updates").checked = s.updatesOn;
  $("set-updates").disabled = !s.releaseBuild;
  if (!s.releaseBuild) say($("set-updates-msg"), "This is a development build: it doesn't check for updates.");
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

$("set-updates").onchange = async (e) => {
  try {
    await api("SetUpdates", e.target.checked);
    await refresh();
    say($("set-updates-msg"), "");
  } catch (err) {
    e.target.checked = !e.target.checked;
    say($("set-updates-msg"), errText(err), "bad");
  }
};

$("set-updates-now").onclick = (e) => busy(e.currentTarget, async () => {
  try {
    say($("set-updates-msg"), await api("CheckUpdate"), "ok");
  } catch (err) {
    say($("set-updates-msg"), errText(err), "bad");
  }
});

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
  $("qr").innerHTML = `<svg viewBox="0 0 ${q.size} ${q.size}" shape-rendering="crispEdges"><path fill="#050f16" d="${q.path}"/></svg>`;
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
      const { text, copied } = await api("TestStop");
      const q = $("wz-heard");
      if (text.trim()) {
        q.textContent = text;
        q.hidden = false;
        setTry("ready", copied ? "That's what Rambler heard. It's on the clipboard too."
          : "That's what Rambler heard, but it couldn't be copied to the clipboard.");
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
  // A dictation just ended: the open Stats page counts it.
  if (ev.data.state === "idle" && !$("page-stats").hidden && !$("main").hidden) loadStats();
});
Events.On("state", () => { if (state) refresh(); });

await refresh();
if (state.phone) {
  showMain();
  // ?page=stats opens on that page (for the browser preview).
  const page = new URLSearchParams(location.search).get("page");
  if (page && $("page-" + page)) showPage(page);
  checkConnection();
} else {
  openWizard("install", false);
}
