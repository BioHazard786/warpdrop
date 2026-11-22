package info.warplabs.warpdrop

import info.warplabs.warpdrop.core.*
import info.warplabs.warpdrop.ui.filesLabel
import info.warplabs.warpdrop.ui.formatBytes
import info.warplabs.warpdrop.ui.percent
import info.warplabs.warpdrop.ui.SpeedMeter
import info.warplabs.warpdrop.ui.eta
import info.warplabs.warpdrop.ui.formatEta
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import kotlin.test.*

class FormatTest {
    @Test fun bytes() {
        assertEquals("0 B", formatBytes(0))
        assertEquals("1023 B", formatBytes(1023))
        assertEquals("1 KB", formatBytes(1024))
        assertEquals("1.5 KB", formatBytes(1536))
        assertEquals("2 GB", formatBytes(2L * 1024 * 1024 * 1024))
    }

    @Test fun speedMeterSmoothsAndIgnoresTinyGaps() {
        val m = SpeedMeter()
        assertEquals(0.0, m.sample(0, 1_000))
        assertEquals(1000.0, m.sample(1000, 2_000)) // first reading is taken as is
        assertEquals(1000.0, m.sample(1500, 2_100)) // < 200 ms later: unchanged
        assertEquals(1000.0 * 0.7 + 2000.0 * 0.3, m.sample(3000, 3_000)) // 2000 B over 1 s, blended
        val before = m.speed
        assertEquals(before, m.sample(100, 4_000)) // the count went down (resume): re-baseline, keep the speed
        assertEquals(before * 0.7 + 1000.0 * 0.3, m.sample(1100, 5_000))
    }

    @Test fun etaFormats() {
        assertEquals("--", formatEta(0.0))
        assertEquals("45s", formatEta(44.2))
        assertEquals("3m 20s", formatEta(200.0))
        assertEquals("1h 5m", formatEta(3900.0))
        assertEquals(10.0, eta(1000, 100.0))
        assertEquals(0.0, eta(1000, 0.0))
    }

    @Test fun filesLabelPluralizes() {
        assertEquals("1 file", filesLabel(1))
        assertEquals("2 files", filesLabel(2))
        assertEquals("0 files", filesLabel(0))
    }

    @Test fun percentIsClamped() {
        assertEquals(0f, percent(5, 0))
        assertEquals(1f, percent(15, 10))
        assertEquals(0.5f, percent(5, 10))
    }
}

class ReceiveStateTest {
    private val offer = listOf(OfferedFile("a", 100, ""), OfferedFile("b", 50, ""))

    @Test fun countsFinishedFilesInFullAndTheCurrentOneByProgress() {
        val s = ReceiveState(offer = offer, done = setOf(0), received = mapOf(0 to 100L, 1 to 20L))
        assertEquals(120, s.receivedBytes)
        assertEquals(150, s.total)
    }

    @Test fun successNeedsEveryFileAndNoError() {
        assertFalse(ReceiveState(offer = offer, done = setOf(0), ended = true).succeeded)
        assertFalse(ReceiveState(offer = offer, done = setOf(0, 1), ended = true, error = "boom").succeeded)
        assertFalse(ReceiveState(offer = offer, done = setOf(0, 1)).succeeded) // still running
        assertTrue(ReceiveState(offer = offer, done = setOf(0, 1), ended = true).succeeded)
    }
}

class PeerStatusTest {
    @Test fun unknownStatusFallsBackToConnecting() {
        assertEquals(PeerStatus.Sending, PeerStatus.parse("sending"))
        assertEquals(PeerStatus.Connecting, PeerStatus.parse("something new"))
        assertEquals(ReceiveStatus.Reconnecting, ReceiveStatus.parse("reconnecting"))
    }
}

@OptIn(ExperimentalCoroutinesApi::class)
class AppControllerTest {
    private class File(override val name: String, override val size: Long = 10) : PickedFile { override val mime = "" }

    private class FakeSend : SendSession {
        override val state = MutableStateFlow(SendState())
        var cancelled = false
        override fun cancel() { cancelled = true; state.value = state.value.copy(ended = true) }
    }

    private class FakeReceive : ReceiveSession {
        override val state = MutableStateFlow(ReceiveState())
        override fun accept() {}
        override fun decline() {}
        override fun cancel() { state.value = state.value.copy(ended = true) }
    }

    private class FakeNearby(val onIncoming: (ReceiveSession) -> Unit) : NearbySession {
        override val state = MutableStateFlow(NearbyState(devices = listOf(NearbyDevice("d1", "Phone", true))))
        var listed = true
        var stopped = false
        val sentTo = mutableListOf<String>()
        override fun setVisible(visible: Boolean) { listed = visible }
        override fun sendTo(files: List<PickedFile>, deviceId: String) = FakeSend().also { sentTo += deviceId }
        override fun stop() { stopped = true }
    }

    private class FakeCore : WarpCore {
        var nearby: FakeNearby? = null
        var nearbyStarts = 0
        var nearbyStoppedBefore = false
        override fun startNearby(settings: Settings, deviceName: String, visible: Boolean, onIncoming: (ReceiveSession) -> Unit): NearbySession {
            nearbyStoppedBefore = nearby?.stopped == true
            nearbyStarts++
            return FakeNearby(onIncoming).also { nearby = it }
        }
        val sends = mutableListOf<FakeSend>()
        val receives = mutableListOf<Pair<String, FakeReceive>>()
        override fun startSend(files: List<PickedFile>, settings: Settings, deviceName: String) = FakeSend().also { sends += it }
        override fun startReceive(code: String, settings: Settings, deviceName: String) = FakeReceive().also { receives += code to it }
    }

    private val store = object : SettingsStore {
        val s = MutableStateFlow(Settings())
        override val settings = s
        override suspend fun save(settings: Settings) { s.value = settings }
    }

    private fun TestScope.controller(core: WarpCore) = AppController(core, store, "test", CoroutineScope(StandardTestDispatcher(testScheduler)))

    @Test fun nearbyJoinsOnceAndFollowsTheScreen() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.nearbyOn(); c.nearbyOn()
        assertEquals(1, core.nearbyStarts)
        testScheduler.advanceUntilIdle()
        assertEquals(listOf("Phone"), c.nearbyState.value.devices.map { it.name })
        c.nearbyOff()
        testScheduler.advanceUntilIdle()
        assertEquals(true, core.nearby!!.stopped)
    }

    @Test fun theListMovesWhenTheServerSettingLoadsOrChanges() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.nearbyOn()
        testScheduler.advanceUntilIdle()
        assertEquals(1, core.nearbyStarts) // same server as the default: no rejoin
        store.s.value = store.s.value.copy(server = "http://lan:8090")
        testScheduler.advanceUntilIdle()
        assertEquals(2, core.nearbyStarts)
        assertEquals(true, core.nearbyStoppedBefore)
    }

    @Test fun visibilityIsSavedAndPassedOn() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.nearbyOn()
        c.setVisible(false)
        testScheduler.advanceUntilIdle()
        assertEquals(false, store.s.value.nearbyVisible)
        assertEquals(false, core.nearby!!.listed)
    }

    @Test fun severalDevicesCanSendToUsAtOnce() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.nearbyOn()
        val first = FakeReceive()
        val second = FakeReceive()
        core.nearby!!.onIncoming(first)
        core.nearby!!.onIncoming(second)
        assertEquals(listOf<ReceiveSession>(first, second), c.receives.value)
        c.closeReceive(first)
        assertEquals(listOf<ReceiveSession>(second), c.receives.value)
    }

    @Test fun sendingToANearbyDeviceNeedsFilesAndUsesThatDevice() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.nearbyOn()
        c.sendNearby("d1")
        assertEquals(0, core.nearby!!.sentTo.size)
        c.addFiles(listOf(File("a")))
        c.sendNearby("d1")
        c.sendNearby("d1") // already sending
        c.sendNearby("d2") // another device at the same time
        assertEquals(listOf("d1", "d2"), core.nearby!!.sentTo)
        assertEquals(setOf("d1", "d2"), c.nearbySends.value.keys)
        assertNull(c.send.value) // nearby sends never take over the screen
    }

    @Test fun filesStayWhileANearbySendRunsAndCancelledOnesCanBeResent() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.nearbyOn()
        c.addFiles(listOf(File("a")))
        c.sendNearby("d1")
        advanceUntilIdle()
        c.clearFiles()
        assertEquals(1, c.picked.value.size) // refused: still sending
        c.cancelNearby("d1")
        advanceUntilIdle()
        c.sendNearby("d1") // the old one ended, so this is a fresh send
        assertEquals(listOf("d1", "d1"), core.nearby!!.sentTo)
        c.cancelNearby("d1")
        advanceUntilIdle()
        c.clearFiles()
        assertTrue(c.picked.value.isEmpty())
        assertTrue(c.nearbySends.value.isEmpty())
    }

    @Test fun duplicatePicksAreIgnored() = runTest {
        val c = controller(FakeCore())
        c.addFiles(listOf(File("a"), File("b")))
        c.addFiles(listOf(File("a"), File("c")))
        assertEquals(listOf("a", "b", "c"), c.picked.value.map { it.name })
    }

    @Test fun sendNeedsFilesAndStartsOnlyOnce() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.startSend()
        assertEquals(0, core.sends.size)
        c.addFiles(listOf(File("a")))
        c.startSend(); c.startSend()
        assertEquals(1, core.sends.size)
    }

    @Test fun closingTheSendScreenStopsTheShareAndClearsFiles() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.addFiles(listOf(File("a")))
        c.startSend()
        c.closeSend()
        assertTrue(core.sends[0].cancelled)
        assertNull(c.send.value)
        assertTrue(c.picked.value.isEmpty())
    }

    @Test fun receivesByCodeAreTrimmedAndCanRunTogether() = runTest {
        val core = FakeCore()
        val c = controller(core)
        c.startReceive("  4821-amber-otter-kite-moth ")
        c.startReceive("other")
        assertEquals(listOf("4821-amber-otter-kite-moth", "other"), core.receives.map { it.first })
        assertEquals(2, c.receives.value.size)
    }

    @Test fun backLeavesOneScreenAtATime() = runTest {
        val c = controller(FakeCore())
        assertFalse(c.back())
        c.addFiles(listOf(File("a")))
        assertTrue(c.back()) // clears the picked list
        assertTrue(c.picked.value.isEmpty())
        c.startReceive("x")
        assertTrue(c.back())
        assertTrue(c.receives.value.isEmpty())
    }

    @Test fun busyFollowsTheSessions() = runTest {
        val core = FakeCore()
        val c = controller(core)
        advanceUntilIdle()
        assertFalse(c.busy.value)
        c.startReceive("x")
        advanceUntilIdle()
        assertTrue(c.busy.value)
        core.receives[0].second.cancel()
        advanceUntilIdle()
        assertFalse(c.busy.value)
    }
}
