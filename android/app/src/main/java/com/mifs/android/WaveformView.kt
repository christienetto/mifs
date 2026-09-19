package com.mifs.android

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.view.MotionEvent
import android.view.View
import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sqrt

class WaveformView(context: Context, val selection: Selection, private val levels: FloatArray) : View(context) {
    var centerToken = -1
    var playhead: Int? = null
    var changed: (() -> Unit)? = null
    var finished: (() -> Unit)? = null
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private var left = 0f
    private var mode = 0
    private var downX = 0f
    private var original = 0
    private val span get() = min(30_000, selection.total).toFloat()
    private val inset = 8 * resources.displayMetrics.density
    private val area get() = max(1f, width - 2 * inset)
    private fun x(time: Int) = inset + (time - left) / span * area
    fun center() {
        left = (selection.start + selection.length / 2 - span / 2).coerceIn(0f, max(0f, selection.total - span))
        invalidate()
    }
    init {
        contentDescription = "Song waveform. Drag the selection edges to trim, or drag the waveform to move."
        isFocusable = true
    }
    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val mid = height * .5f
        val step = 4 * resources.displayMetrics.density
        var px = inset
        while (px < width - inset) {
            val ms = left + (px - inset) / area * span
            val idx = (ms / selection.total * levels.size).toInt().coerceIn(0, max(0, levels.size - 1))
            val level = if (levels.isEmpty()) .08f else sqrt(levels[idx].coerceIn(0f, 1f))
            val bar = max(3f, level * height * .36f)
            paint.color = if (ms >= selection.start && ms <= selection.end) Color.WHITE else 0x55FFFFFF
            canvas.drawRoundRect(px, mid - bar, px + step * .55f, mid + bar, 3f, 3f, paint)
            px += step
        }
        paint.color = 0x18FFFFFF
        canvas.drawRoundRect(x(selection.start), 12f, x(selection.end), height - 12f, 14f, 14f, paint)
        paint.color = 0xFFCEC1FF.toInt()
        paint.style = Paint.Style.STROKE; paint.strokeWidth = 2 * resources.displayMetrics.density
        canvas.drawRoundRect(x(selection.start), 12f, x(selection.end), height - 12f, 14f, 14f, paint)
        paint.style = Paint.Style.FILL
        playhead?.let { time ->
            paint.color = Color.WHITE
            canvas.drawLine(x(time), 8f, x(time), height - 8f, paint)
        }
        paint.color = 0xFFCEC1FF.toInt()
        for (edge in listOf(selection.start, selection.end)) {
            canvas.drawRoundRect(x(edge)-6f, mid-22f, x(edge)+6f, mid+22f, 6f, 6f, paint)
        }
    }
    override fun onTouchEvent(event: MotionEvent): Boolean {
        when(event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                parent.requestDisallowInterceptTouchEvent(true)
                downX = event.x; original = selection.start
                val threshold = 26 * resources.displayMetrics.density
                val startDistance = abs(event.x - x(selection.start))
                val endDistance = abs(event.x - x(selection.end))
                mode = if (min(startDistance, endDistance) < threshold) {
                    if (startDistance < endDistance) 1 else 2
                } else 3
                return true
            }
            MotionEvent.ACTION_MOVE -> {
                val time = (left + (event.x - inset) / area * span).toInt()
                when(mode) {
                    1 -> selection.startAt(time)
                    2 -> selection.endAt(time)
                    else -> selection.move(original + ((event.x - downX) / area * span).toInt())
                }
                changed?.invoke(); invalidate(); return true
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                parent.requestDisallowInterceptTouchEvent(false)
                center(); finished?.invoke(); performClick(); return true
            }
        }
        return super.onTouchEvent(event)
    }
    override fun performClick(): Boolean { super.performClick(); return true }
}
