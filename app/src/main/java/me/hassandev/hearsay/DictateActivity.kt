package me.hassandev.hearsay

import android.app.Activity
import android.content.Intent
import android.graphics.drawable.GradientDrawable
import android.media.AudioManager
import android.media.AudioRecordingConfiguration
import android.media.MediaRecorder
import android.os.Bundle
import android.os.SystemClock
import android.os.Handler
import android.os.Looper
import android.text.Editable
import android.text.InputType
import android.text.TextWatcher
import android.view.Gravity
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsAnimation
import android.view.WindowManager
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import java.lang.ref.WeakReference

/**
 * Dictation screen for Rambler to type into, styled after M3 Expressive: a status mark that
 * morphs between shapes, a headline + supporting line, and the text field in a container.
 * Every change is mirrored to [text] and [changedAt] for Session.
 *
 * "Listening" means something is recording for voice recognition (Rambler, in practice),
 * from AudioManager's recording callback, so Session doesn't have to tell us.
 *
 * Extras: `clear` (boolean) empties the field.
 */
class DictateActivity : Activity() {

    private enum class State { WAITING, LISTENING, DONE }

    private lateinit var palette: Palette
    private lateinit var field: EditText
    private lateinit var mark: StatusShapeView
    private lateinit var headline: TextView
    private lateinit var supporting: TextView
    private val audio by lazy { getSystemService(AudioManager::class.java) }
    private var listening = false
    private var state: State? = null
    private var imeAnimating = false

    private val recordingCallback = object : AudioManager.AudioRecordingCallback() {
        override fun onRecordingConfigChanged(configs: List<AudioRecordingConfiguration>) =
            setListening(configs)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        palette = Palette(this)
        applySurface(palette)
        setContentView(buildLayout())
        current = WeakReference(this)
        handle(intent)
    }

    override fun onDestroy() {
        if (current.get() === this) current = WeakReference(null)
        super.onDestroy()
    }

    override fun onStart() {
        super.onStart()
        audio.registerAudioRecordingCallback(recordingCallback, Handler(Looper.getMainLooper()))
        setListening(audio.activeRecordingConfigurations)
    }

    override fun onStop() {
        keyboardSettled = false
        audio.unregisterAudioRecordingCallback(recordingCallback)
        super.onStop()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handle(intent)
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (hasFocus) showKeyboard()
    }

    private fun buildLayout(): View {
        mark = StatusShapeView(this)
        val appName = textView(TextStyle.LABEL_LARGE, palette.primary, "Hearsay")
        headline = textView(TextStyle.HEADLINE_MEDIUM_EMPHASIZED, palette.onSurface).apply {
            accessibilityLiveRegion = View.ACCESSIBILITY_LIVE_REGION_POLITE
        }
        supporting = textView(TextStyle.BODY_MEDIUM, palette.onSurfaceVariant)
        val titles = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            addView(appName)
            addView(headline)
            addView(supporting)
        }
        val header = LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            addView(mark, LinearLayout.LayoutParams(dp(80), dp(80)).apply { marginEnd = dp(16) })
            addView(titles, LinearLayout.LayoutParams(0, -2, 1f))
        }

        field = EditText(this).apply {
            inputType = InputType.TYPE_CLASS_TEXT or
                InputType.TYPE_TEXT_FLAG_MULTI_LINE or
                InputType.TYPE_TEXT_FLAG_CAP_SENTENCES
            gravity = Gravity.TOP or Gravity.START
            hint = "Your words will appear here"
            TextStyle.TITLE_LARGE.applyTo(this)
            setTextColor(palette.onSurface)
            setHintTextColor(palette.onSurfaceVariant)
            highlightColor = palette.primaryContainer
            textCursorDrawable?.setTint(palette.primary)
            background = GradientDrawable().apply {
                cornerRadius = dp(28).toFloat()
                setColor(palette.surfaceContainer)
            }
            setPadding(dp(24), dp(20), dp(24), dp(20))
            addTextChangedListener(object : TextWatcher {
                override fun beforeTextChanged(s: CharSequence?, a: Int, b: Int, c: Int) {}
                override fun onTextChanged(s: CharSequence?, a: Int, b: Int, c: Int) {}
                override fun afterTextChanged(s: Editable?) {
                    textChanged(s?.toString().orEmpty())
                    updateState()
                }
            })
        }

        return LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            addView(header, LinearLayout.LayoutParams(-1, -2))
            addView(field, LinearLayout.LayoutParams(-1, 0, 1f).apply {
                topMargin = dp(24)
                bottomMargin = dp(16)
            })
            // Edge to edge (targetSdk 35+): keep content clear of the bars and the keyboard.
            setOnApplyWindowInsetsListener { v, insets ->
                val bars = insets.getInsets(WindowInsets.Type.systemBars() or
                    WindowInsets.Type.displayCutout() or WindowInsets.Type.ime())
                v.setPadding(bars.left + dp(16), bars.top + dp(24), bars.right + dp(16), bars.bottom)
                // Shown without an animation (e.g. it was already up when we were relaunched).
                if (!imeAnimating) keyboardSettled = insets.isVisible(WindowInsets.Type.ime())
                WindowInsets.CONSUMED
            }
            setWindowInsetsAnimationCallback(imeAnimationCallback)
        }
    }

    // The keyboard's accessibility bounds are its final position, so a tap while it is still
    // sliding in lands on the wrong spot. KeyPressService waits for this to settle.
    private val imeAnimationCallback =
        object : WindowInsetsAnimation.Callback(DISPATCH_MODE_CONTINUE_ON_SUBTREE) {
            override fun onPrepare(animation: WindowInsetsAnimation) {
                if (animation.typeMask and WindowInsets.Type.ime() == 0) return
                imeAnimating = true
                keyboardSettled = false
            }

            override fun onProgress(insets: WindowInsets, running: List<WindowInsetsAnimation>) = insets

            override fun onEnd(animation: WindowInsetsAnimation) {
                if (animation.typeMask and WindowInsets.Type.ime() == 0) return
                imeAnimating = false
                keyboardSettled = window.decorView.rootWindowInsets?.isVisible(WindowInsets.Type.ime()) == true
            }
        }

    private fun handle(intent: Intent) {
        if (intent.getBooleanExtra("clear", false)) field.setText("")
        // setText("") on an already-empty field doesn't fire the watcher.
        textChanged(field.text.toString())
        updateState()
        showKeyboard()
    }

    private fun setListening(configs: List<AudioRecordingConfiguration>) {
        listening = configs.any { it.clientAudioSource == MediaRecorder.AudioSource.VOICE_RECOGNITION }
        updateState()
    }

    private fun updateState() {
        if (!::field.isInitialized) return
        val next = when {
            listening -> State.LISTENING
            field.text.isNotEmpty() -> State.DONE
            else -> State.WAITING
        }
        if (next == state) return
        state = next
        val mic = getDrawable(R.drawable.ic_mic)!!
        when (next) {
            State.WAITING -> {
                headline.text = "Ready"
                supporting.text = "Waiting for Rambler…"
                mark.show(StatusShapeView.Look(0, 0f, palette.surfaceContainerHighest, palette.onSurfaceVariant, mic))
            }
            State.LISTENING -> {
                headline.text = "Listening"
                supporting.text = "Talk to your phone, then press the hotkey on your computer again"
                mark.show(StatusShapeView.Look(9, 0.07f, palette.primary, palette.onPrimary, mic))
            }
            State.DONE -> {
                // The text goes to the computer once Rambler has finished rewriting it.
                headline.text = "Got it"
                supporting.text = "Sending the text to your computer…"
                mark.show(StatusShapeView.Look(4, 0.12f, palette.tertiaryContainer,
                    palette.onTertiaryContainer, getDrawable(R.drawable.ic_check)!!))
            }
        }
        mark.spinning = next == State.LISTENING
        mark.contentDescription = headline.text
    }

    private fun showKeyboard() {
        field.requestFocus()
        field.post { field.windowInsetsController?.show(WindowInsets.Type.ime()) }
    }

    private fun textChanged(text: String) {
        Companion.text = text
        changedAt = SystemClock.uptimeMillis()
    }

    companion object {
        /** True while this screen is showing and the keyboard has finished sliding in. */
        @Volatile
        var keyboardSettled = false
            private set

        /** The field's text. */
        @Volatile
        var text = ""
            private set
        /** When [text] last changed, in SystemClock.uptimeMillis (immune to clock changes). */
        @Volatile
        var changedAt = 0L
            private set

        private var current = WeakReference<DictateActivity>(null)

        /** Empties the field if the screen exists. Call on the main thread. */
        fun clear() {
            current.get()?.let { it.field.setText("") } ?: run { text = "" }
        }
    }
}
