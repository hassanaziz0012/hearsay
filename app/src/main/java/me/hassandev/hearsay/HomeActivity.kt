package me.hassandev.hearsay

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.graphics.drawable.GradientDrawable
import android.net.ConnectivityManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.view.Gravity
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import java.net.Inet4Address

/**
 * The launcher screen: whether the key presser and the local network permission are on,
 * the phone's address, and the paired computers. While it is showing, a computer can pair
 * (`hearsay pair`), and the code to compare comes up here.
 */
class HomeActivity : Activity() {

    private lateinit var palette: Palette
    private lateinit var content: LinearLayout
    private val handler = Handler(Looper.getMainLooper())
    private var askedPermission = false

    private val refresher = object : Runnable {
        override fun run() {
            render()
            handler.postDelayed(this, 1000)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        palette = Palette(this)
        window.decorView.setBackgroundColor(palette.surface)
        if (!palette.dark) {
            val light = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS or
                WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS
            window.insetsController?.setSystemBarsAppearance(light, light)
        }
        content = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        setContentView(ScrollView(this).apply {
            addView(content)
            // Edge to edge (targetSdk 35+): keep content clear of the bars.
            setOnApplyWindowInsetsListener { v, insets ->
                val bars = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.displayCutout())
                v.setPadding(bars.left + dp(16), bars.top + dp(24), bars.right + dp(16), bars.bottom + dp(16))
                WindowInsets.CONSUMED
            }
        })
        if (!localNetworkAllowed()) requestLocalNetwork()
    }

    override fun onResume() {
        super.onResume()
        PhoneServer.pairingPrompt = PhoneServer.PairingPrompt(::askPair)
        // Setup over adb grants the local network permission without asking (pm grant).
        if (localNetworkAllowed() && !PhoneServer.advertised) KeyPressService.instance?.server?.advertise()
        handler.post(refresher)
    }

    override fun onPause() {
        PhoneServer.pairingPrompt = null
        handler.removeCallbacks(refresher)
        super.onPause()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, results: IntArray) {
        // mDNS registration was refused without it; try again now.
        if (localNetworkAllowed()) KeyPressService.instance?.server?.advertise()
        render()
    }

    private fun askPair(name: String, code: String, answer: (Boolean) -> Unit): () -> Unit {
        var dialog: AlertDialog? = null
        var answered = false
        val reply = { yes: Boolean ->
            if (!answered) {
                answered = true
                answer(yes)
            }
        }
        runOnUiThread {
            dialog = AlertDialog.Builder(this)
                .setTitle("Pair with $name?")
                .setMessage("Check that your computer shows the same code:\n\n${code.take(3)} ${code.drop(3)}")
                .setPositiveButton("Pair") { _, _ -> reply(true) }
                .setNegativeButton("Cancel") { _, _ -> reply(false) }
                .setOnDismissListener { reply(false); render() }
                .show()
        }
        return { runOnUiThread { dialog?.dismiss() } }
    }

    private fun render() {
        content.removeAllViews()
        content.addView(text(TextStyle.LABEL_LARGE, palette.primary, "Hearsay"))
        content.addView(text(TextStyle.HEADLINE_MEDIUM_EMPHASIZED, palette.onSurface, "Dictate from your computer"))
        content.addView(text(TextStyle.BODY_MEDIUM, palette.onSurfaceVariant,
            "Your computer starts Rambler here and gets the text back over Wi-Fi."), spaced(0, 24))

        val service = KeyPressService.instance
        card("Key presser",
            if (service != null) "On" else "Off. Turn on “Hearsay key presser” in Accessibility settings.",
            if (service != null) null else "Open settings" to {
                startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
            })
        if (service == null && sideloaded()) {
            card("If Android says it's a restricted setting",
                "Open App info, tap ⋮ at the top, then Allow restricted settings. Then turn the key presser on again.",
                "App info" to {
                    startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", packageName, null)))
                })
        }
        if (Build.VERSION.SDK_INT >= 37) {
            val allowed = localNetworkAllowed()
            card("Local network",
                if (allowed) "Allowed" else "Not allowed. Your computer can't reach the phone without it.",
                if (allowed) null else "Allow" to ::requestLocalNetwork)
        }
        val address = wifiAddress()
        card("This phone", when {
            service == null || PhoneServer.port == 0 -> "Not reachable while the key presser is off."
            address == null -> "Not connected to a network."
            else -> "$address, port ${PhoneServer.port}" +
                if (PhoneServer.advertised) "" else "\nNot announced on the network, so pair by address."
        })

        content.addView(text(TextStyle.TITLE_LARGE, palette.onSurface, "Paired computers"), spaced(16, 8))
        val paired = PairedComputers(this).all()
        if (paired.isEmpty()) {
            content.addView(text(TextStyle.BODY_MEDIUM, palette.onSurfaceVariant,
                "None yet. Keep this screen open and run `hearsay setup` (or `hearsay pair`) on your computer."))
        }
        for ((fp, name) in paired) {
            card(name, "Key ${fp.take(8)}…", "Forget" to {
                PairedComputers(this).remove(fp)
                render()
            })
        }
    }

    private fun card(title: String, body: String, action: Pair<String, () -> Unit>? = null) {
        val column = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            addView(text(TextStyle.LABEL_LARGE, palette.onSurface, title))
            addView(text(TextStyle.BODY_MEDIUM, palette.onSurfaceVariant, body))
        }
        val row = LinearLayout(this).apply {
            gravity = Gravity.CENTER_VERTICAL
            background = GradientDrawable().apply {
                cornerRadius = dp(20).toFloat()
                setColor(palette.surfaceContainer)
            }
            setPadding(dp(20), dp(16), dp(16), dp(16))
            addView(column, LinearLayout.LayoutParams(0, -2, 1f))
            if (action != null) addView(button(action.first, action.second))
        }
        content.addView(row, spaced(0, 8))
    }

    private fun button(label: String, onClick: () -> Unit) = Button(this).apply {
        text = label
        isAllCaps = false
        TextStyle.LABEL_LARGE.applyTo(this)
        setTextColor(palette.onPrimary)
        background = GradientDrawable().apply {
            cornerRadius = dp(20).toFloat()
            setColor(palette.primary)
        }
        minHeight = dp(40)
        minimumHeight = dp(40)
        setPadding(dp(16), 0, dp(16), 0)
        stateListAnimator = null
        setOnClickListener { onClick() }
    }

    // Installed from a downloaded file: Android blocks turning on its accessibility service
    // until the user allows restricted settings in App info. (adb installs aren't blocked.)
    private fun sideloaded() = packageManager.getInstallSourceInfo(packageName).packageSource.let {
        it == PackageInstaller.PACKAGE_SOURCE_DOWNLOADED_FILE || it == PackageInstaller.PACKAGE_SOURCE_LOCAL_FILE
    }

    private fun localNetworkAllowed() = Build.VERSION.SDK_INT < 37 ||
        checkSelfPermission(LOCAL_NETWORK) == PackageManager.PERMISSION_GRANTED

    private fun requestLocalNetwork() {
        if (askedPermission && !shouldShowRequestPermissionRationale(LOCAL_NETWORK)) {
            // Denied for good: only App info can change it now.
            startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", packageName, null)))
            return
        }
        askedPermission = true
        requestPermissions(arrayOf(LOCAL_NETWORK), 1)
    }

    private fun wifiAddress(): String? {
        val cm = getSystemService(ConnectivityManager::class.java)
        return cm.getLinkProperties(cm.activeNetwork)?.linkAddresses
            ?.map { it.address }?.firstOrNull { it is Inet4Address }?.hostAddress
    }

    private fun spaced(top: Int, bottom: Int) = LinearLayout.LayoutParams(-1, -2).apply {
        topMargin = dp(top)
        bottomMargin = dp(bottom)
    }

    private fun text(style: TextStyle, color: Int, value: String) = TextView(this).apply {
        style.applyTo(this)
        setTextColor(color)
        text = value
        textAlignment = View.TEXT_ALIGNMENT_VIEW_START
    }

    private fun dp(v: Int) = (v * resources.displayMetrics.density).toInt()

    private companion object {
        // Android 17 (API 37): needed to talk to devices on the local network.
        const val LOCAL_NETWORK = "android.permission.ACCESS_LOCAL_NETWORK"
    }
}
