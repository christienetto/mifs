package com.mifs.android

import org.junit.Assert.*
import org.junit.Test

class SelectionTest {
    @Test fun shortSongAndOutOfBounds() {
        val s = Selection(1500, 90_000)
        assertEquals(0, s.start); assertEquals(1500, s.length)
        s.resize(20_000); s.move(-500)
        assertEquals(0, s.start); assertEquals(1500, s.end)
    }
    @Test fun handlesPreserveOppositeEdgeAndLimits() {
        val s = Selection(60_000, 30_000)
        s.startAt(0)
        assertEquals(20_000, s.start); assertEquals(40_000, s.end)
        s.startAt(50_000)
        assertEquals(1000, s.length); assertEquals(40_000, s.end)
        s.endAt(90_000)
        assertEquals(59_000, s.end); assertEquals(20_000, s.length)
        s.move(100_000)
        assertEquals(60_000, s.end)
    }
    @Test fun resizingCentersAndClamps() {
        val s = Selection(60_000, 25_000, 10_000)
        s.resize(20_000)
        assertEquals(20_000, s.start); assertEquals(40_000, s.end)
        s.move(59_000); s.resize(1000)
        assertTrue(s.end <= s.total); assertEquals(1000, s.length)
    }
}
