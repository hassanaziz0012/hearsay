// Stand-in for the Wails runtime when index.html is opened in a plain browser, to work
// on the design. ?setup opens the wizard; ?state=listening (or busy, error) sets the status;
// ?page=stats opens on that page; ?update shows a new release, ?phoneapp an
// outdated phone app.

const q = new URLSearchParams(location.search);
const paired = !q.has("setup");
const st = q.get("state") || "idle";
const labels = { idle: "Ready", busy: "Starting…", listening: "Listening", error: "Error", retry: "Reconnect the phone" };

const state = {
  os: q.get("os") || "linux", wayland: false, desktopShortcuts: false,
  phone: paired ? { name: "Hearsay on Pixel 9", addr: "192.168.18.57:47800" } : null,
  keyboard: "Gboard", keyboardId: "com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME",
  hotkey: "ctrl+alt+space", hotkeyName: "Ctrl+Alt+Space", hotkeyLine: "Hotkey: Ctrl+Alt+Space", hotkeyError: "",
  paste: true, statsOn: true, pasteForcedOff: false, autostart: true, autostartOther: false,
  status: { state: st, label: labels[st], detail: "", lastError: st === "error" ? "The phone didn't answer in time. Check that it's on and on the same Wi-Fi as this computer." : "" },
  configPath: "/home/you/.config/hearsay/config.json", logPath: "/run/user/1000/hearsay/hearsay.log",
  releaseUrl: "https://github.com/hassanaziz0012/hearsay/releases/latest/download/hearsay.apk",
  toggleCommand: "/usr/local/bin/hearsay toggle", version: q.has("update") ? "v0.3.0" : "dev",
  releaseBuild: q.has("update"), updatesOn: true,
  phoneApp: q.has("phoneapp") ? { version: "0.3.0", outdated: true, newer: false } : null,
  update: q.has("update") ? { version: "v0.4.0", page: "https://github.com/hassanaziz0012/hearsay/releases/latest", canInstall: true } : null,
};

// A year of made-up dictations for the Stats page; ?stats=empty shows it before the first.
function mockStats() {
  const empty = q.get("stats") === "empty";
  const days = [];
  const now = new Date();
  const back = 53 * 7 + ((now.getDay() + 6) % 7);
  let seed = 7;
  const rnd = () => (seed = (seed * 16807) % 2147483647) / 2147483647;
  for (let i = back; i >= 0; i--) {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i, 12);
    const quiet = empty || rnd() < (d.getDay() % 6 === 0 ? 0.7 : 0.2) || i > 300;
    const dictations = quiet ? 0 : 1 + Math.floor(rnd() * 14);
    days.push({ date: d.toISOString().slice(0, 10), words: Math.round(dictations * (20 + rnd() * 90)), dictations });
  }
  const sum = (list) => list.reduce((s, d) => ({ words: s.words + d.words, dictations: s.dictations + d.dictations }), { words: 0, dictations: 0 });
  const total = sum(days);
  const byHour = Array.from({ length: 24 }, (_, h) => (empty || h < 8 ? 0 : Math.round(total.words / 14 * Math.exp(-((h - 15) ** 2) / 18))));
  const byWeekday = [0.18, 0.2, 0.17, 0.19, 0.16, 0.04, 0.06].map((f) => Math.round(total.words * f));
  const best = days.reduce((a, b) => (b.words > a.words ? b : a));
  const typingWpm = state.typingWpm || 40;
  const speakMs = total.words / 132 * 60000;
  return {
    today: sum(days.slice(-1)), week: sum(days.slice(-((now.getDay() + 6) % 7) - 1)), month: sum(days.slice(-now.getDate())), total,
    days, byHour, byWeekday, streak: empty ? 0 : 6, longestStreak: empty ? 0 : 23,
    bestDay: empty ? null : best, longestWords: empty ? 0 : 412, longestDate: best.date,
    speakMs, paceWpm: 132, typingWpm, typingMeasured: !!state.typingWpm,
    savedMs: Math.max(0, total.words / typingWpm * 60000 - speakMs),
    cancelled: empty ? 0 : 31, empty: empty ? 0 : 12, since: empty ? "" : days.find((d) => d.dictations)?.date,
  };
}

const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const methods = {
  State: () => state,
  InstallQR: () => ({ size: 5, path: "M0 0h5v5h-5z" }),
  FindPhones: async () => { await wait(1500); return [{ name: "Hearsay on Pixel 9", addr: "192.168.18.57:47800" }]; },
  CheckAddress: (a) => ({ name: "phone", addr: a + ":47800" }),
  PairStart: async () => { await wait(800); return "482 913"; },
  PairFinish: async () => { await wait(2500); state.phone = { name: "Hearsay on Pixel 9", addr: "192.168.18.57:47800" }; },
  Keyboards: () => [
    { id: "com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME", label: "Gboard", enabled: true, mic: true, selected: false },
    { id: "com.akshaykadam.pixelboard/com.android.inputmethod.latin.LatinIME", label: "Pixelboard", enabled: true, mic: false, selected: false },
  ],
  TestStart: () => wait(600),
  TestStop: async () => { await wait(900); return { text: "Remind me to send the invoice to Sam before Friday.", copied: true }; },
  CheckConnection: async () => { await wait(600); return "Pixel 9 answers."; },
  Forget: () => "Forgotten on both sides.",
  Stats: mockStats,
  SetTypingSpeed: (wpm) => { state.typingWpm = wpm; },
  SetStats: (on) => { state.statsOn = on; },
  ExportStats: () => "/home/you/hearsay-stats.csv",
  ResetStats: () => { q.set("stats", "empty"); },
  SaveReport: () => "/home/you/hearsay-report.txt",
  CheckUpdate: async () => { await wait(700); return state.update ? `Hearsay ${state.update.version} is out.` : "Hearsay dev is the latest version."; },
  InstallUpdate: async () => { await wait(2000); },
  SetUpdates: (on) => { state.updatesOn = on; },
};

export const Call = {
  ByName: async (name, ...args) => {
    const fn = methods[name.replace("main.API.", "")];
    return fn ? fn(...args) : undefined;
  },
};
export const Events = { On: () => () => {} };
export const Browser = { OpenURL: (u) => window.open(u) };
