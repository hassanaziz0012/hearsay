# Prelude for the scripts in this folder. Each script runs on the phone in one `adb shell`
# (mksh), so its polling loops cost no adb round trips. They exit 0 on success and 10-19
# for their own failures; anything else is adb failing. Inputs come as variables set before
# this prelude: IME (target keyboard component), PKG, ACT, X/Y (calibrated mic key, empty
# if none), D (1 = debug lines on stderr), plus per-script ones.
#
# Keep apostrophes out of these files, comments included (they used to live in
# single-quoted bash strings, and phone_test.go checks for them).

# mksh arithmetic is 32-bit (with defined wraparound), so keep times relative to the start.
T=$(date +%s%3N)
now() { echo $(( $(date +%s%3N) - T )); }
dbg() { [ "$D" = 1 ] && echo "  phone +$(now)ms $*" >&2; return 0; }
listening() { dumpsys audio 2>/dev/null | grep -q "client=VOICE_RECOGNITION.*pack:${IME%%/*}"; }
# press <mic|close> via the accessibility service of the app: 1 pressed, 2 key not on
# screen, 0 service off.
press() { r=$(am broadcast -a $PKG.PRESS -p $PKG --es key "$1" --es ime "${IME%%/*}"); r=${r##*result=}; echo "${r%%[!0-9]*}"; }
# out <status|text>: the app output (see OutReceiver), text as base64.
out() {
    r=$(am broadcast -a $PKG.OUT -p $PKG --es what "$1")
    case $r in *result=1*) ;; *) return 1;; esac
    r=${r#*data=\"}; echo "${r%\"*}"
}
# Tap at the calibrated mic position; fails if there is none.
tap_mic() { [ -n "$X" ] && input tap $X $Y; }
# Android 17 has mInputShown + mIsInputViewShown; older versions may lack the second one,
# so only a field that is present and false counts against it.
kbd_shown() {
    d=$(dumpsys input_method 2>/dev/null)
    case $d in *mInputShown=true*) ;; *) return 1;; esac
    case $d in *mIsInputViewShown=false*) case $d in *mIsInputViewShown=true*) ;; *) return 1;; esac;; esac
    return 0
}
