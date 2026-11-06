package me.hassandev.hearsay

import android.app.Activity
import android.content.res.Configuration
import android.graphics.Typeface
import android.util.TypedValue
import android.widget.TextView

/** M3 type scale entries used by the app's screens: size, line height (sp), tracking (sp), weight. */
internal enum class TextStyle(val size: Float, val lineHeight: Float, val tracking: Float, val weight: Int) {
    HEADLINE_MEDIUM_EMPHASIZED(28f, 36f, 0f, 500),
    TITLE_LARGE(22f, 28f, 0f, 400),
    BODY_MEDIUM(14f, 20f, 0.2f, 400),
    LABEL_LARGE(14f, 20f, 0.1f, 500);

    fun applyTo(view: TextView) {
        view.setTextSize(TypedValue.COMPLEX_UNIT_SP, size)
        view.setLineHeight(TypedValue.COMPLEX_UNIT_SP, lineHeight)
        view.letterSpacing = tracking / size
        view.typeface = Typeface.create(Typeface.DEFAULT, weight, false)
    }
}

/** M3 colour roles from the device's dynamic (Material You) scheme, light or dark. */
internal class Palette(private val activity: Activity) {
    val dark = activity.resources.configuration.uiMode and
        Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES
    private fun c(light: Int, dark: Int) = activity.getColor(if (this.dark) dark else light)

    val surface = c(android.R.color.system_surface_light, android.R.color.system_surface_dark)
    val surfaceContainer = c(android.R.color.system_surface_container_light, android.R.color.system_surface_container_dark)
    val surfaceContainerHighest = c(android.R.color.system_surface_container_highest_light, android.R.color.system_surface_container_highest_dark)
    val onSurface = c(android.R.color.system_on_surface_light, android.R.color.system_on_surface_dark)
    val onSurfaceVariant = c(android.R.color.system_on_surface_variant_light, android.R.color.system_on_surface_variant_dark)
    val primary = c(android.R.color.system_primary_light, android.R.color.system_primary_dark)
    val onPrimary = c(android.R.color.system_on_primary_light, android.R.color.system_on_primary_dark)
    val primaryContainer = c(android.R.color.system_primary_container_light, android.R.color.system_primary_container_dark)
    val tertiaryContainer = c(android.R.color.system_tertiary_container_light, android.R.color.system_tertiary_container_dark)
    val onTertiaryContainer = c(android.R.color.system_on_tertiary_container_light, android.R.color.system_on_tertiary_container_dark)
}
