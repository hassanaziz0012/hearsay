package me.hassandev.hearsay

import android.accessibilityservice.AccessibilityService
import android.content.Context
import android.content.Intent
import android.media.AudioManager
import android.media.MediaRecorder
import android.os.Handler
import android.os.PowerManager
import android.os.SystemClock
import android.util.Log
import android.view.inputmethod.InputMethodManager
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * One dictation at a time, run for PhoneServer's requests: open the field, switch to the
 * keyboard with Rambler, press its mic key; on stop, wait for the text to settle; then put
 * the phone back.
 *
 * The phone owns the session (the keyboard to go back to, whether the screen was on), so a
 * dropped connection loses nothing: the next press asks what state it is in. It is also
 * saved to preferences, so if the process dies mid-dictation the next press still puts the
 * phone back (phase INTERRUPTED).
 *
 * Methods block and must not run on the main thread. PhoneServer runs one at a time.
 */
class Session(private val service: KeyPressService) {

    enum class Phase { IDLE, STARTING, LISTENING, INTERRUPTED }

    /** A failure the PC reports to the user: `code` for the PC, `msg` for people. */
    class Failure(val code: String, msg: String) : Exception(msg)

    @Volatile
    var phase = Phase.IDLE
        private set

    private val prefs = service.getSharedPreferences("session", Context.MODE_PRIVATE)
    private val audio = service.getSystemService(AudioManager::class.java)
    private val main = Handler(service.mainLooper)
    private var t0 = 0L
    private val trace = mutableListOf<String>()

    init {
        if (prefs.getBoolean("active", false)) phase = Phase.INTERRUPTED
    }

    /** Timings of the last request, for the PC's debug output. */
    fun takeTrace(): List<String> = trace.toList().also { trace.clear() }

    fun beginRequest() {
        t0 = SystemClock.uptimeMillis()
        trace.clear()
    }

    private fun dbg(msg: String) {
        val line = "+${SystemClock.uptimeMillis() - t0}ms $msg"
        trace += line
        Log.i(TAG, line)
    }

    /**
     * Opens the dictation field on the target keyboard `ime` (an IME id, package/class) and
     * starts Rambler. On failure the phone is put back before the Failure is thrown.
     */
    fun start(ime: String) {
        if (phase == Phase.LISTENING) return
        if (phase == Phase.INTERRUPTED) restore()
        val current = service.getSystemService(InputMethodManager::class.java).currentInputMethodInfo?.id
        // Already on the target keyboard (Rambler in stock Gboard): nothing to switch back to.
        // An unknown current keyboard still gets switched, just not switched back.
        val prev = current?.takeIf { it != ime }
        val awake = service.getSystemService(PowerManager::class.java).isInteractive
        // commit, not apply: if the process dies right after, the next press must still
        // find the session to put the phone back (INTERRUPTED).
        prefs.edit().putBoolean("active", true).putString("prev", prev).putBoolean("awake", awake).commit()
        phase = Phase.STARTING
        dbg("prev ime=$prev awake=$awake")
        try {
            if (current != ime && !service.softKeyboardController.switchToInputMethod(ime)) {
                throw Failure("switch_failed", "couldn't switch to the keyboard $ime. Is it enabled on the phone?")
            }
            // turnScreenOn and showWhenLocked bring it up over the lock screen. A bound
            // accessibility service may start activities from the background.
            service.startActivity(Intent(service, DictateActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK).putExtra("clear", true))
            dbg("field open")
            val pkg = ime.substringBefore('/')
            val opened = SystemClock.uptimeMillis()
            // Press the mic key once the keyboard is up (press() refuses until it has slid
            // in), but not before FIRST_PRESS_MS: the keyboard ignores the mic key for a while
            // after it comes up. In 16 starts, every press 704-848 ms after the field opened
            // was lost and every press from 855 ms on worked, however long the keyboard had
            // been settled. If Rambler isn't recording RETRY_MS after a press (a cold
            // keyboard, after a reboot), press once more.
            val keyboardBy = SystemClock.uptimeMillis() + 6000
            var listenBy = 0L
            var pressedAt = 0L
            var presses = 0
            var lastMiss: String? = null
            while (!listening()) {
                val now = SystemClock.uptimeMillis()
                if (now - opened >= FIRST_PRESS_MS && (pressedAt == 0L || (presses < 2 && now - pressedAt > RETRY_MS))) {
                    val missed = press("mic", pkg)
                    if (missed == null) {
                        pressedAt = SystemClock.uptimeMillis()
                        if (listenBy == 0L) listenBy = pressedAt + 4500
                        dbg("pressed mic (${++presses})")
                    } else if (missed != lastMiss) {
                        lastMiss = missed
                        dbg("mic not pressed: $missed")
                    }
                }
                if (listenBy == 0L && now > keyboardBy) throw Failure("keyboard_not_up", "the keyboard didn't come up")
                if (listenBy != 0L && now > listenBy) throw Failure("not_started", "Rambler didn't start listening")
                SystemClock.sleep(50)
            }
            phase = Phase.LISTENING
            dbg("listening")
        } catch (e: Exception) {
            restore()
            throw e
        }
    }

    /**
     * Stops Rambler. With `paste`, presses "Insert text" and waits until the field has text
     * that has stopped changing for `settleMs` (Rambler may rewrite it), giving up after
     * `timeoutMs`; otherwise presses "Close Rambler", which discards it. Calls `reply` with
     * the text ("" when cancelled or nothing was heard), or with null when nothing was
     * listening but an interrupted session was put back. Then puts the phone back, so the
     * PC can paste without waiting for that.
     */
    fun stop(paste: Boolean, ime: String, settleMs: Long, timeoutMs: Long, reply: (String?) -> Unit) {
        when (phase) {
            Phase.IDLE, Phase.STARTING -> throw Failure("not_listening", "not listening")
            Phase.INTERRUPTED -> {
                restore()
                return reply(null)
            }
            Phase.LISTENING -> {}
        }
        // Rambler may already have stopped on its own; pressing then would start a new session.
        if (listening()) {
            val key = if (paste) "mic" else "close"
            val missed = press(key, ime.substringBefore('/'))
            dbg(if (missed == null) "pressed $key" else "$key not pressed: $missed")
        }
        if (!paste) {
            reply("")
            return restore()
        }
        val end = SystemClock.uptimeMillis() + timeoutMs
        var arrived = DictateActivity.text.isNotEmpty()
        // When Rambler stopped recording: the text comes after that, but if none has come
        // NOTHING_MS later, nothing was heard.
        var stoppedAt = 0L
        while (SystemClock.uptimeMillis() < end) {
            if (!arrived && DictateActivity.text.isNotEmpty()) {
                arrived = true
                dbg("text arrived")
            }
            val now = SystemClock.uptimeMillis()
            if (listening()) {
                stoppedAt = 0L
            } else if (stoppedAt == 0L) {
                stoppedAt = now
            }
            val quiet = now - DictateActivity.changedAt
            if (stoppedAt != 0L) {
                if (DictateActivity.text.isNotEmpty() && quiet >= settleMs) break
                if (DictateActivity.text.isEmpty() && now - stoppedAt >= NOTHING_MS) {
                    dbg("nothing heard")
                    break
                }
            }
            SystemClock.sleep(50)
        }
        val text = DictateActivity.text
        dbg("got ${text.length} chars")
        try {
            reply(text)
        } finally {
            restore()
        }
    }

    /** Clears the field and puts the keyboard and screen back as start found them. */
    fun restore() {
        onMain { DictateActivity.clear() }
        service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_HOME)
        prefs.getString("prev", null)?.let { service.softKeyboardController.switchToInputMethod(it) }
        if (!prefs.getBoolean("awake", true)) {
            service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_LOCK_SCREEN)
        }
        prefs.edit().clear().apply()
        phase = Phase.IDLE
        dbg("restored")
    }

    /** Is something recording for voice recognition (Rambler, in practice)? */
    private fun listening() = audio.activeRecordingConfigurations
        .any { it.clientAudioSource == MediaRecorder.AudioSource.VOICE_RECOGNITION }

    /** KeyPressService.press, waited for: null when pressed, else why it wasn't. */
    private fun press(key: String, pkg: String): String? {
        var result: String? = "no answer from the key presser"
        awaitOnMain { done -> service.press(key, pkg) { missed -> result = missed; done() } }
        return result
    }

    /** Runs `block` on the main thread and waits (up to 2 s) until it calls `done`. */
    private fun awaitOnMain(block: (done: () -> Unit) -> Unit) {
        val latch = CountDownLatch(1)
        main.post { block(latch::countDown) }
        latch.await(2, TimeUnit.SECONDS)
    }

    private fun onMain(block: () -> Unit) = awaitOnMain { done -> block(); done() }

    private companion object {
        const val TAG = "Hearsay"
        const val FIRST_PRESS_MS = 1000
        const val RETRY_MS = 2000
        const val NOTHING_MS = 5000L
    }
}
