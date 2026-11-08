// Stand-in for the Wails runtime when index.html is opened in a plain browser, to work
// on the design. ?setup opens the wizard; ?state=listening (or busy, error) sets the status.

const q = new URLSearchParams(location.search);
const paired = !q.has("setup");
const st = q.get("state") || "idle";
const labels = { idle: "Ready", busy: "Starting…", listening: "Listening", error: "Error", retry: "Reconnect the phone" };

const state = {
  os: q.get("os") || "linux", wayland: false, desktopShortcuts: false,
  phone: paired ? { name: "Hearsay on Pixel 9", addr: "192.168.18.57:47800" } : null,
  keyboard: "Gboard", keyboardId: "com.google.android.inputmethod.latin/com.android.inputmethod.latin.LatinIME",
  hotkey: "ctrl+alt+space", hotkeyName: "Ctrl+Alt+Space", hotkeyLine: "Hotkey: Ctrl+Alt+Space", hotkeyError: "",
  paste: true, pasteForcedOff: false, autostart: true, autostartOther: false,
  status: { state: st, label: labels[st], detail: "", lastError: st === "error" ? "The phone didn't answer in time. Check that it's on and on the same Wi-Fi as this computer." : "" },
  configPath: "/home/you/.config/hearsay/config.json", logPath: "/run/user/1000/hearsay/hearsay.log",
  releaseUrl: "https://github.com/hassanaziz0012/hearsay/releases/latest/download/hearsay.apk",
  toggleCommand: "/usr/local/bin/hearsay toggle", version: "dev",
};

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
  TestStop: async () => { await wait(900); return "Remind me to send the invoice to Sam before Friday."; },
  CheckConnection: async () => { await wait(600); return "Pixel 9 answers."; },
  Forget: () => "Forgotten on both sides.",
  SaveReport: () => "/home/you/hearsay-report.txt",
};

export const Call = {
  ByName: async (name, ...args) => {
    const fn = methods[name.replace("main.API.", "")];
    return fn ? fn(...args) : undefined;
  },
};
export const Events = { On: () => () => {} };
export const Browser = { OpenURL: (u) => window.open(u) };
