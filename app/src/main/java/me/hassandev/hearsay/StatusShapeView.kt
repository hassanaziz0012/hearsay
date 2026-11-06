package me.hassandev.hearsay

import android.animation.ArgbEvaluator
import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.drawable.Drawable
import android.view.View
import android.view.animation.LinearInterpolator
import android.view.animation.PathInterpolator
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin

/**
 * The status mark: a filled shape with an icon in it. Each status has its own silhouette
 * (circle, scalloped "cookie", soft square-ish cookie), and changes morph between them
 * the way M3 Expressive morphs its library shapes. While [spinning] it rotates slowly.
 *
 * Silhouettes are radial: r(θ) = 1 + amplitude·cos(lobes·θ), normalised to fit. Morphing
 * interpolates r(θ) point by point, so any two silhouettes blend smoothly.
 */
class StatusShapeView(context: Context) : View(context) {

    data class Look(val lobes: Int, val amplitude: Float, val fill: Int, val iconTint: Int, val icon: Drawable)

    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val path = Path()
    private var from: Look? = null
    private var to: Look? = null
    private var progress = 1f
    private var fill = 0
    private var iconAlpha = 1f
    private var rotationDeg = 0f

    // M3 Expressive "default spatial" (shape) and "default effects" (colour, opacity), as
    // the published cubic-bezier fallbacks for springs.
    private val morph = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 500
        interpolator = PathInterpolator(0.38f, 1.21f, 0.22f, 1.00f)
        addUpdateListener { progress = it.animatedValue as Float; invalidate() }
    }
    private val effects = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 200
        interpolator = PathInterpolator(0.34f, 0.80f, 0.34f, 1.00f)
        addUpdateListener {
            val t = it.animatedValue as Float
            fill = ArgbEvaluator().evaluate(t, from?.fill ?: 0, to!!.fill) as Int
            iconAlpha = t
            invalidate()
        }
    }
    private val spin = ValueAnimator.ofFloat(0f, 360f).apply {
        duration = 12_000
        interpolator = LinearInterpolator()
        repeatCount = ValueAnimator.INFINITE
        addUpdateListener { rotationDeg = it.animatedValue as Float; invalidate() }
    }

    var spinning = false
        set(value) {
            field = value
            if (value && isAttachedToWindow) spin.start() else spin.cancel()
        }

    fun show(look: Look) {
        if (to == null) {
            to = look; fill = look.fill; progress = 1f; iconAlpha = 1f
            invalidate()
            return
        }
        if (look == to) return
        from = to
        to = look
        morph.start()
        effects.start()
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (spinning) spin.start()
    }

    override fun onDetachedFromWindow() {
        spin.cancel()
        super.onDetachedFromWindow()
    }

    override fun onDraw(canvas: Canvas) {
        val look = to ?: return
        val cx = width / 2f
        val cy = height / 2f
        val radius = min(width, height) / 2f

        path.rewind()
        val steps = 240
        for (i in 0..steps) {
            val theta = 2 * PI * i / steps + Math.toRadians(rotationDeg.toDouble())
            val r1 = from?.let { shape(it, theta) } ?: shape(look, theta)
            val r = r1 + (shape(look, theta) - r1) * progress
            val x = cx + (radius * r * cos(theta)).toFloat()
            val y = cy + (radius * r * sin(theta)).toFloat()
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        path.close()
        paint.color = fill
        canvas.drawPath(path, paint)

        // The new icon fades in over the old one (effects: no overshoot).
        val iconSize = (radius * 0.9f).toInt()
        // Same icon in both looks: only its tint changes.
        fun drawIcon(icon: Drawable, tint: Int, alpha: Float) {
            icon.mutate().setTint(tint)
            icon.alpha = (alpha * 255).toInt()
            icon.setBounds((cx - iconSize / 2).toInt(), (cy - iconSize / 2).toInt(),
                (cx + iconSize / 2).toInt(), (cy + iconSize / 2).toInt())
            icon.draw(canvas)
        }
        val prev = from
        when {
            prev == null || iconAlpha >= 1f -> drawIcon(look.icon, look.iconTint, 1f)
            prev.icon === look.icon -> drawIcon(look.icon,
                ArgbEvaluator().evaluate(iconAlpha, prev.iconTint, look.iconTint) as Int, 1f)
            else -> {
                drawIcon(prev.icon, prev.iconTint, 1f - iconAlpha)
                drawIcon(look.icon, look.iconTint, iconAlpha)
            }
        }
    }

    private fun shape(look: Look, theta: Double) =
        (1 + look.amplitude * cos(look.lobes * theta)) / (1 + look.amplitude)
}
