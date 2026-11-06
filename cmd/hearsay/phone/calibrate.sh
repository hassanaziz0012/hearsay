# Brings up the target keyboard in the dictation field, for a UI dump.
input keyevent KEYCODE_WAKEUP
ime set $IME >/dev/null && am start -n $ACT --ez clear true >/dev/null 2>&1 || exit 10
end=$(( $(now) + 4000 ))
until kbd_shown; do [ $(now) -gt $end ] && exit 11; sleep 0.1; done
sleep 0.3
