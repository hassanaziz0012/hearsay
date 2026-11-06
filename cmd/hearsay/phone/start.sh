# Opens the dictation field and starts Rambler. PREV: the keyboard to go back to later,
# empty when the target is already the current one (then nothing is switched).
input keyevent KEYCODE_WAKEUP
[ -z "$PREV" ] || ime set $IME >/dev/null || exit 10
am start -n $ACT --ez clear true >/dev/null 2>&1 || exit 10
dbg "field open"
# Press the mic key once the keyboard is up: by resource id through the service, else at
# the calibrated coordinates. Also fall back if the service is on but still cannot see the
# key a second after the keyboard is up (seen once right after enabling it).
end=$(( $(now) + 4000 )); shown_at=
until listening; do
    r=$(press mic)
    [ "$r" = 1 ] && { dbg "pressed mic (service)"; break; }
    [ "$r" = 0 ] && [ -z "$X" ] && exit 13
    if kbd_shown; then
        [ -z "$shown_at" ] && shown_at=$(now)
        if [ -n "$X" ] && { [ "$r" = 0 ] || [ $(( $(now) - shown_at )) -gt 1000 ]; }; then
            [ "$r" = 0 ] && sleep 0.3 # let the keyboard finish laying out
            tap_mic; dbg "tapped mic at $X,$Y (service result $r)"; break
        fi
    fi
    [ $(now) -gt $end ] && exit 11
    sleep 0.1
done
# If the press was lost (Rambler not recording 2 s later), press once more.
retry=$(( $(now) + 2000 )); end=$(( $(now) + 4500 ))
until listening; do
    if [ -n "$retry" ] && [ $(now) -gt $retry ]; then
        retry=; r=$(press mic)
        [ "$r" = 1 ] || tap_mic
        dbg "pressed mic again (service result $r)"
    fi
    [ $(now) -gt $end ] && exit 12
    sleep 0.1
done
