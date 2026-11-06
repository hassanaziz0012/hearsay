# Clears the field and puts the phone back: keyboard PREV (empty = leave it), and back to
# sleep if AWAKE=0. -W: the activity turns the screen on, so if it was not open yet (a start
# cut off early) it must be up before the sleep key, or it wakes the phone again.
am start -W -n $ACT --ez clear true >/dev/null 2>&1
input keyevent KEYCODE_HOME
[ -n "$PREV" ] && [ "$PREV" != null ] && ime set "$PREV" >/dev/null
if [ "$AWAKE" = 0 ]; then input keyevent KEYCODE_SLEEP; fi
