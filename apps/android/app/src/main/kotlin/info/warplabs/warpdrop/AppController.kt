package info.warplabs.warpdrop

import info.warplabs.warpdrop.core.*
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.launch

/**
 * App-wide state. Sessions live here, not in a screen, so a transfer survives rotation and the Android
 * service can watch [busy]. The visible screen is derived from which session exists.
 */
class AppController(
    private val core: WarpCore,
    private val store: SettingsStore,
    /** What other devices see, shown as "You'll appear as". */
    val deviceName: String,
    private val scope: CoroutineScope,
) {
    val settings: StateFlow<Settings> = store.settings.stateIn(scope, SharingStarted.Eagerly, Settings())

    private val _picked = MutableStateFlow<List<PickedFile>>(emptyList())
    val picked: StateFlow<List<PickedFile>> = _picked

    /** A share-code send; only one at a time, it takes over the Send tab. */
    private val _send = MutableStateFlow<SendSession?>(null)
    val send: StateFlow<SendSession?> = _send

    /** Nearby sends by device id, one per tapped device, all at once. Finished ones stay until the files are cleared. */
    private val _nearbySends = MutableStateFlow<Map<String, SendSession>>(emptyMap())
    val nearbySends: StateFlow<Map<String, SendSession>> = _nearbySends

    /** Every receive: by code, or another device sending to this one. Several can run at once. */
    private val _receives = MutableStateFlow<List<ReceiveSession>>(emptyList())
    val receives: StateFlow<List<ReceiveSession>> = _receives

    private val _nearby = MutableStateFlow<NearbySession?>(null)
    private var foreground = false

    /** Devices on the network and whether this one is listed; empty while the app is in the background. */
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    val nearbyState: StateFlow<NearbyState> =
        _nearby.flatMapLatest { it?.state ?: flowOf(NearbyState()) }.stateIn(scope, SharingStarted.Eagerly, NearbyState())

    /** Live nearby sends: any not ended. */
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    val sendingNearby: StateFlow<Boolean> = _nearbySends.flatMapLatest { m -> anyLive(m.values.map { it.state.map { s -> !s.ended } }) }
        .stateIn(scope, SharingStarted.Eagerly, false)

    /** Every nearby send's receiver, for the progress list under the device tiles. */
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    val nearbyReceivers: StateFlow<List<Receiver>> = _nearbySends.flatMapLatest { m ->
        if (m.isEmpty()) flowOf(emptyList()) else combine(m.values.map { it.state }) { states -> states.flatMap { it.receivers } }
    }.stateIn(scope, SharingStarted.Eagerly, emptyList())

    /** True while a transfer is running; the Android foreground service follows it. */
    @OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
    val busy: StateFlow<Boolean> = combine(
        _send.flatMapLatest { it?.state?.map { s -> !s.ended } ?: flowOf(false) },
        sendingNearby,
        _receives.flatMapLatest { l -> anyLive(l.map { it.state.map { s -> !s.ended } }) },
    ) { a, b, c -> a || b || c }.stateIn(scope, SharingStarted.Eagerly, false)

    private fun anyLive(flows: List<Flow<Boolean>>): Flow<Boolean> =
        if (flows.isEmpty()) flowOf(false) else combine(flows) { it.any { live -> live } }

    /**
     * Joins the nearby list while the app is on screen (it costs battery and, when visible, lets strangers on the
     * network offer files). A transfer that is still running keeps it alive until it ends.
     */
    fun nearbyOn() {
        foreground = true
        if (_nearby.value != null) return
        startNearby(settings.value)
    }

    private var nearbyServer = ""

    private fun startNearby(s: Settings) {
        nearbyServer = s.server
        _nearby.value = core.startNearby(s, deviceName, s.nearbyVisible, ::incoming)
    }

    init {
        // The list lives on one server. The saved setting loads after the app has already joined (with the
        // default), and the user can change it later: either way, rejoin on the right server.
        scope.launch {
            store.settings.collect { s ->
                val n = _nearby.value ?: return@collect
                if (s.server == nearbyServer) return@collect
                n.stop()
                startNearby(s)
            }
        }
    }

    fun nearbyOff() {
        foreground = false
        scope.launch {
            busy.first { !it }
            if (foreground) return@launch // came back to the app while the transfer ran
            _nearby.value?.stop()
            _nearby.value = null
        }
    }

    fun setVisible(visible: Boolean) {
        saveSettings(settings.value.copy(nearbyVisible = visible))
        _nearby.value?.setVisible(visible)
    }

    private fun incoming(session: ReceiveSession) = _receives.update { it + session }

    /** Sends the picked files to a device from [nearbyState]; each device gets its own transfer. */
    fun sendNearby(deviceId: String) {
        val files = _picked.value
        val n = _nearby.value
        if (files.isEmpty() || n == null || _nearbySends.value[deviceId]?.state?.value?.ended == false) return
        val s = runCatching { n.sendTo(files, deviceId) }.getOrNull() ?: return // the device may have just gone away
        _nearbySends.update { it + (deviceId to s) }
    }

    /** Stops the nearby send to that device; its tile then reads Cancelled. */
    fun cancelNearby(deviceId: String) { _nearbySends.value[deviceId]?.cancel() }

    fun addFiles(files: List<PickedFile>) {
        _picked.update { cur -> cur + files.filter { f -> cur.none { it.name == f.name && it.size == f.size } } }
    }

    fun removeFile(file: PickedFile) = _picked.update { it - file }

    /** Clears the picked files and the finished nearby sends (refused while one is still running). */
    fun clearFiles() {
        if (sendingNearby.value) return
        _picked.value = emptyList()
        _nearbySends.value = emptyMap()
    }

    fun startSend() {
        val files = _picked.value
        if (files.isEmpty() || _send.value != null) return
        _send.value = core.startSend(files, settings.value, deviceName)
    }

    /** [code] is a share code or a link, exactly as typed, scanned or tapped. */
    fun startReceive(code: String) {
        _receives.update { it + core.startReceive(code.trim(), settings.value, deviceName) }
    }

    /** Leaves the send screen (stopping the share). The picked files are cleared once the share is over. */
    fun closeSend() {
        _send.value?.cancel()
        _send.value = null
        _picked.value = emptyList()
    }

    /** Stops [session] if it is still running and takes it off the Receive tab. */
    fun closeReceive(session: ReceiveSession) {
        session.cancel()
        _receives.update { it - session }
    }

    /** Takes every finished receive off the Receive tab. */
    fun clearReceived() = _receives.update { l -> l.filterNot { it.state.value.ended } }

    /** The notification's Stop: ends everything. */
    fun stopAll() {
        closeSend()
        _nearbySends.value.values.forEach { it.cancel() }
        _receives.value.forEach { it.cancel() }
        _receives.value = emptyList()
    }

    /** Back button: leave whichever transfer screen is open. Returns false when already on Home. */
    fun back(): Boolean = when {
        _send.value != null -> { closeSend(); true }
        _receives.value.isNotEmpty() -> { _receives.value.lastOrNull()?.let(::closeReceive); true }
        _picked.value.isNotEmpty() && !sendingNearby.value -> { clearFiles(); true }
        else -> false
    }

    fun saveSettings(s: Settings) {
        scope.launch { store.save(s) }
    }
}
