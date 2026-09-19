package com.mifs.android

/** All editor operations preserve a 1–20 second range inside the recording. */
class Selection(val total: Int, start: Int = 0, length: Int = 10_000) {
    init { require(total >= 1000) { "Audio must be at least one second long." } }
    var length = length.coerceIn(1000, minOf(20_000, total)); private set
    var start = start.coerceIn(0, total - this.length); private set
    val end get() = start + length
    fun move(value: Int) { start = value.coerceIn(0, total - length) }
    fun resize(value: Int) {
        val center = start + length / 2
        length = value.coerceIn(1000, minOf(20_000, total))
        move(center - length / 2)
    }
    fun startAt(value: Int) {
        val oldEnd = end
        start = value.coerceIn(maxOf(0, oldEnd - 20_000), oldEnd - 1000)
        length = oldEnd - start
    }
    fun endAt(value: Int) { length = value.coerceIn(start + 1000, minOf(total, start + 20_000)) - start }
}
