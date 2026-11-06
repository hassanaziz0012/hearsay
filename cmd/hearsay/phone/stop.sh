# Stops Rambler (KEY=mic inserts the text, KEY=close discards it), then for KEY=mic waits
# until the field has text that has stopped changing for SETTLE ms (Rambler may rewrite
# it), giving up after TIMEOUT ms, and prints it.
# Rambler may already have stopped on its own; pressing then would start a new session.
if listening; then
    r=$(press $KEY)
    if [ "$r" = 1 ]; then dbg "pressed $KEY (service)"
    elif tap_mic; then dbg "tapped mic at $X,$Y"
    else dbg "could not press $KEY (service result $r)"; fi
fi
[ $KEY = mic ] || exit 0
end=$(( $(now) + TIMEOUT )); last=; since=$(now)
while [ $(now) -lt $end ]; do
    cur=$(out status)
    t=$(now)
    if [ "$cur" != "$last" ]; then
        last=$cur; since=$t; dbg "changed: $cur"
    elif [ -n "$cur" ] && [ "${cur##* }" != 0 ] && [ $(( t - since )) -ge $SETTLE ] && ! listening; then
        break
    fi
    sleep 0.05
done
t=$(out text) || exit 10
printf %s "$t" | base64 -d
