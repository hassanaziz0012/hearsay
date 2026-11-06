package me.hassandev.hearsay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Base64
import java.io.File

/**
 * Hands DictateActivity's output to the PC side as the broadcast's result data:
 *
 *   adb shell am broadcast -a me.hassandev.hearsay.OUT \
 *       -p me.hassandev.hearsay --es what status|text
 *
 * "status" returns out.ts (last-change time in ms, a space, the text's length), "text"
 * returns out.txt as base64 (no newlines or quotes to escape in `am`'s output). Result 1
 * on success. `am broadcast` takes ~0.13 s where `content read` takes ~1.2 s, which
 * matters while polling for the text to settle.
 *
 * The manifest guards it with android.permission.DUMP, which the adb shell holds and other
 * apps can't get, so dictated text stays private to the PC.
 */
class OutReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        val file = { name: String -> File(context.filesDir, name).takeIf { it.exists() } }
        when (intent.getStringExtra("what")) {
            "status" -> resultData = file("out.ts")?.readText() ?: "0 0"
            "text" -> resultData = Base64.encodeToString(
                file("out.txt")?.readBytes() ?: ByteArray(0), Base64.NO_WRAP)
            else -> return run { resultCode = 3 }
        }
        resultCode = 1
    }
}
