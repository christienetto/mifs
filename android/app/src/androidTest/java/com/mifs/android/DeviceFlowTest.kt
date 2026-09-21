package com.mifs.android

import android.content.Intent
import android.media.MediaMetadataRetriever
import android.net.Uri
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.Until
import androidx.test.uiautomator.UiScrollable
import androidx.test.uiautomator.UiSelector
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import java.io.File

@RunWith(AndroidJUnit4::class)
class DeviceFlowTest {
    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private val context = instrumentation.targetContext

    @Test fun importedAudioUsesRealWaveformAndExportsSelectedDuration() = runBlocking {
        val file = File(context.cacheDir, "test-tone.m4a")
        instrumentation.context.assets.open("tone.m4a").use { input -> file.outputStream().use { input.copyTo(it) } }
        val (duration, waveform) = LocalAudio.analyze(context, Uri.fromFile(file))
        assertTrue(duration in 3900..4200)
        assertTrue(waveform.any { it > .2f })
        val clip = LocalAudio.export(context, Uri.fromFile(file), 1000, 2000)
        val retriever = MediaMetadataRetriever()
        try {
            retriever.setDataSource(clip.absolutePath)
            val length = retriever.extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)!!.toInt()
            assertTrue("Export was $length ms", length in 1900..2150)
        } finally { retriever.release(); clip.delete(); file.delete() }
    }

    @Test fun catalogEditorShareAndRecent() {
        val device = UiDevice.getInstance(instrumentation)
        context.startActivity(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK))
        assertTrue(device.wait(Until.hasObject(By.text("Top Songs")), 15_000))
        UiScrollable(UiSelector().resourceId("discover-list")).scrollIntoView(UiSelector().resourceId("song-neon-harbor"))
        assertTrue(device.wait(Until.hasObject(By.res("song-neon-harbor")), 15_000))
        device.findObject(By.res("song-neon-harbor")).click()
        assertTrue(device.wait(Until.hasObject(By.res("length-5")), 15_000))
        device.findObject(By.res("length-5")).click()
        assertTrue(device.wait(Until.hasObject(By.desc("Stop preview")), 15_000))
        device.waitForIdle()
        device.takeScreenshot(File(context.getExternalFilesDir(null), "editor.png"))
        device.findObject(By.res("share")).click()
        assertTrue(device.wait(Until.hasObject(By.res("share-telegram")), 15_000))
        device.findObject(By.res("share-other")).click()
        assertTrue(device.wait(Until.hasObject(By.res("android", "content_preview_text")), 15_000))
        assertTrue(device.findObject(By.res("android", "content_preview_text")).text.contains("/m/"))
        device.pressBack()
        device.pressBack()
        assertTrue(device.wait(Until.hasObject(By.res("nav-recent")), 5000))
        device.findObject(By.res("nav-recent")).click()
        assertTrue(device.wait(Until.hasObject(By.res("mif-neon-harbor")), 5000))
        device.takeScreenshot(File(context.getExternalFilesDir(null), "recent.png"))
        device.findObject(By.res("mif-neon-harbor")).click()
        assertTrue(device.wait(Until.hasObject(By.text("YOUR MIF")), 5000))
        assertTrue(device.wait(Until.hasObject(By.desc("Stop preview")), 15_000))
        device.pressBack()
    }

    @Test fun selectionSurvivesRotation() {
        val device = UiDevice.getInstance(instrumentation)
        context.startActivity(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK))
        assertTrue(device.wait(Until.hasObject(By.text("Top Songs")), 15_000))
        UiScrollable(UiSelector().resourceId("discover-list")).scrollIntoView(UiSelector().resourceId("song-neon-harbor"))
        device.findObject(By.res("song-neon-harbor")).click()
        assertTrue(device.wait(Until.hasObject(By.res("length-5")), 15_000))
        device.findObject(By.res("length-5")).click()
        assertTrue(device.wait(Until.hasObject(By.text("5s moment")), 5000))
        try {
            device.setOrientationLeft()
            assertTrue(device.wait(Until.hasObject(By.res("editor-wide")), 15_000))
            assertTrue(device.wait(Until.hasObject(By.text("5s moment")), 15_000))
            device.waitForIdle()
            assertTrue(device.hasObject(By.res("share")))
            device.takeScreenshot(File(context.getExternalFilesDir(null), "landscape.png"))
            device.setOrientationNatural()
            assertTrue(device.wait(Until.hasObject(By.res("editor-portrait")), 15_000))
            assertTrue(device.wait(Until.hasObject(By.res("share")), 15_000))
            assertTrue(device.hasObject(By.text("5s moment")))
        } finally { device.setOrientationNatural(); device.unfreezeRotation() }
        device.pressBack()
    }
}
