package me.hassandev.hearsay

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.graphics.Rect
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo

/**
 * Presses the target keyboard's header keys by resource id, wherever the keyboard has put
 * them, so the PC side doesn't depend on fixed tap coordinates. It also hosts PhoneServer,
 * the PC's network connection: the system keeps a bound accessibility service's process at
 * foreground priority, which keeps the socket reachable in Doze.
 *
 * Session presses "mic" (the voice typing key, "Insert text" while Rambler listens) and
 * "close" (the left header key, which reads "Close Rambler" while it listens) of the
 * keyboard that has Rambler (stock Gboard or a Gboard build like Pixelboard).
 *
 * The service reads windows only when asked; it ignores accessibility events.
 */
class KeyPressService : AccessibilityService() {

    var server: PhoneServer? = null
        private set

    override fun onServiceConnected() {
        instance = this
        // Connected again without a destroy in between: the old server still holds the port.
        server?.stop()
        server = PhoneServer(this).also { it.start() }
    }

    override fun onDestroy() {
        instance = null
        server?.stop()
        super.onDestroy()
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {}
    override fun onInterrupt() {}

    /**
     * Taps `key` (mic or close) of the keyboard with package `ime` and calls `done` with
     * null once it is pressed, or with why it wasn't. `done` runs on the main thread, so a
     * caller there must not block waiting for it.
     */
    fun press(key: String, ime: String, done: (String?) -> Unit) {
        val id = KEYS[key] ?: return done("unknown key $key")
        // Same process as DictateActivity, so its flag is readable directly.
        if (!DictateActivity.keyboardSettled) return done("keyboard not settled")
        val bounds = findKey("$ime:id/$id")
            ?: return done("key not on screen; windows " + windows.joinToString { "${it.type}:${it.root?.childCount}" })
        // Keyboards handle raw touches rather than clicks, so tap the key's centre.
        val tap = Path().apply { moveTo(bounds.exactCenterX(), bounds.exactCenterY()) }
        val gesture = GestureDescription.Builder()
            .addStroke(GestureDescription.StrokeDescription(tap, 0, 50))
            .build()
        val callback = object : GestureResultCallback() {
            override fun onCompleted(g: GestureDescription) = done(null)
            override fun onCancelled(g: GestureDescription) = done("gesture cancelled")
        }
        if (!dispatchGesture(gesture, callback, null)) done("gesture refused")
    }

    private fun findKey(id: String): Rect? {
        val node = findNode(id) ?: return null
        return Rect().also(node::getBoundsInScreen).takeUnless { it.isEmpty }
    }

    private fun findNode(id: String): AccessibilityNodeInfo? {
        val ime = windows.firstOrNull { it.type == AccessibilityWindowInfo.TYPE_INPUT_METHOD }
            ?: return null
        return ime.root?.findAccessibilityNodeInfosByViewId(id)?.firstOrNull { it.isVisibleToUser }
    }

    companion object {
        /** The running service, for HomeActivity's status line. */
        @Volatile
        var instance: KeyPressService? = null
            private set

        // Resource ids (without the package prefix) of the keys, the same in every keyboard
        // with Rambler: stock Gboard and Gboard builds like Pixelboard (repackaged Gboard).
        // Stock Gboard's ids while Rambler listens are not yet verified.
        private val KEYS = mapOf(
            "mic" to "key_pos_header_power_key",
            "close" to "key_pos_header_access_points_menu",
        )
    }
}
