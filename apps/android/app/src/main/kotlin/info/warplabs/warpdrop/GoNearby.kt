package info.warplabs.warpdrop

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import info.warplabs.warpdrop.core.*
import info.warplabs.warpdrop.core.mobile.LANHandler
import info.warplabs.warpdrop.core.mobile.Mobile
import info.warplabs.warpdrop.core.mobile.NearbyListener
import info.warplabs.warpdrop.core.mobile.ReceiveListener
import info.warplabs.warpdrop.core.mobile.Sink
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import java.net.Inet4Address
import java.util.UUID
import kotlin.coroutines.resume
import info.warplabs.warpdrop.core.mobile.NearbyJob as GoNearbyJob

private const val SERVICE = "_warpdrop._tcp"

/**
 * The same-network device list. The Go core merges two sources: the server's list (other browsers, CLIs and apps on
 * this public network) and the local network, which Android's NsdManager does for us (no multicast lock, no extra
 * permission beyond local network access). While visible, a TCP port is announced so devices can send to us
 * directly, with no server and no internet.
 *
 * The server fixes whether we are listed when we join, so toggling visibility rebuilds the whole session.
 */
@Suppress("DEPRECATION") // NsdManager.resolveService/host: the replacements need API 34, this app supports 29
internal class GoNearbySession(
    private val context: Context,
    private val scope: CoroutineScope,
    private val settings: Settings,
    private val deviceName: String,
    visible: Boolean,
    private val onIncoming: (ReceiveSession) -> Unit,
) : NearbySession {
    private val _state = MutableStateFlow(NearbyState(visible = visible))
    override val state: StateFlow<NearbyState> = _state

    private val nsd = context.getSystemService(NsdManager::class.java)
    // stable per install: our own announcement from an earlier session (the app was backgrounded, killed or
    // reinstalled) can linger in mDNS caches, and only a matching id tells us it is us
    private val ownId = context.getSharedPreferences("nearby", Context.MODE_PRIVATE).let { p ->
        p.getString("id", null) ?: UUID.randomUUID().toString().replace("-", "").take(16).also { p.edit().putString("id", it).apply() }
    }
    private val lock = Any()
    private var job: GoNearbyJob? = null
    private var registration: NsdManager.RegistrationListener? = null
    private var discovery: NsdManager.DiscoveryListener? = null
    private var resolver: Job? = null
    private val found = Channel<NsdServiceInfo>(Channel.UNLIMITED)
    private val peerOf = HashMap<String, String>() // NSD service name -> peer id, to forget a device that goes away

    init { start(visible) }

    override fun setVisible(visible: Boolean) {
        if (visible != _state.value.visible) start(visible)
    }

    override fun sendTo(files: List<PickedFile>, deviceId: String): SendSession {
        val j = synchronized(lock) { job } ?: error("not looking for devices")
        val d = _state.value.devices.firstOrNull { it.id == deviceId } ?: error("that device is gone")
        return GoSendSession(context, scope, files, settings, deviceName, j, d)
    }

    override fun stop() = synchronized(lock) { teardown() }

    private fun start(visible: Boolean) = synchronized(lock) {
        teardown()
        val j = Mobile.newNearbyJob(settings.server, deviceName, !visible)
        j.useExternalDiscovery()
        job = j
        _state.value = NearbyState(visible = visible)
        j.start(Events(j))
        discover(j)
        if (visible) {
            // no port means nobody can reach us directly, but the server's list and sending still work
            runCatching { j.listenLAN(LanRouter(j)) }.onSuccess { announce(it) }
        }
    }

    private fun teardown() {
        job?.stop()
        job = null
        resolver?.cancel()
        registration?.let { runCatching { nsd.unregisterService(it) } }
        discovery?.let { runCatching { nsd.stopServiceDiscovery(it) } }
        registration = null
        discovery = null
        synchronized(peerOf) { peerOf.clear() }
    }

    // ---- what the Go job reports ----

    private inner class Events(private val j: GoNearbyJob) : NearbyListener {
        private fun current() = synchronized(lock) { job === j }

        override fun onDevices() {
            if (!current()) return
            val devices = (0 until j.deviceCount()).mapNotNull { i ->
                val id = j.deviceID(i)
                if (id.isEmpty()) null else NearbyDevice(id, j.deviceName(i), j.deviceDirect(i))
            }
            _state.update { it.copy(devices = devices) }
        }

        override fun onServer(err: String) {
            if (current()) _state.update { it.copy(server = err) }
        }

        override fun onInvite(deviceID: String, deviceName: String, roomID: String, ticket: String) {
            if (!current()) return
            val rj = Mobile.newReceiveJob(settings.server, this@GoNearbySession.deviceName, false)
            val s = GoReceiveSession(context, rj)
            onIncoming(s)
            scope.launch { rj.startNearby(roomID, ticket, s, s) }
        }

        override fun onEnd(err: String) {}
    }

    /** Each direct connection is its own receive (a few can run at once), stoppable through cancelLAN. */
    private inner class LanRouter(private val j: GoNearbyJob) : LANHandler {
        private val pending = HashMap<String, GoReceiveSession>() // Go asks for the listener, then the sink

        override fun listener(id: String): ReceiveListener =
            GoReceiveSession(context, null) { j.cancelLAN(id) }.also {
                synchronized(pending) { pending[id] = it }
                onIncoming(it)
            }

        override fun sink(id: String): Sink = synchronized(pending) { pending.remove(id) } ?: error("no receive for connection $id")
    }

    // ---- NsdManager ----

    private fun announce(port: Int) {
        val info = NsdServiceInfo().apply {
            serviceName = ownId
            serviceType = SERVICE
            setPort(port)
            setAttribute("v", "2")
            setAttribute("id", ownId)
            setAttribute("name", deviceName.take(100))
            setAttribute("client", "android")
        }
        val l = object : NsdManager.RegistrationListener {
            override fun onServiceRegistered(i: NsdServiceInfo) {}
            override fun onRegistrationFailed(i: NsdServiceInfo, code: Int) {}
            override fun onServiceUnregistered(i: NsdServiceInfo) {}
            override fun onUnregistrationFailed(i: NsdServiceInfo, code: Int) {}
        }
        registration = l
        nsd.registerService(info, NsdManager.PROTOCOL_DNS_SD, l)
    }

    private fun discover(j: GoNearbyJob) {
        // resolving is one at a time on older Android, so found services queue up here
        resolver = scope.launch { for (info in found) resolve(info, j) }
        val l = object : NsdManager.DiscoveryListener {
            override fun onServiceFound(i: NsdServiceInfo) { if (i.serviceName != ownId) found.trySend(i) }
            override fun onServiceLost(i: NsdServiceInfo) {
                val id = synchronized(peerOf) { peerOf.remove(i.serviceName) } ?: return
                j.removeLANPeer(id)
            }
            override fun onDiscoveryStarted(type: String) {}
            override fun onDiscoveryStopped(type: String) {}
            override fun onStartDiscoveryFailed(type: String, code: Int) {}
            override fun onStopDiscoveryFailed(type: String, code: Int) {}
        }
        discovery = l
        nsd.discoverServices(SERVICE, NsdManager.PROTOCOL_DNS_SD, l)
    }

    private suspend fun resolve(info: NsdServiceInfo, j: GoNearbyJob) {
        val r = withTimeoutOrNull(5_000) {
            suspendCancellableCoroutine<NsdServiceInfo?> { c ->
                nsd.resolveService(info, object : NsdManager.ResolveListener {
                    override fun onResolveFailed(i: NsdServiceInfo, code: Int) { c.resume(null) }
                    override fun onServiceResolved(i: NsdServiceInfo) { c.resume(i) }
                })
            }
        } ?: return
        fun attr(k: String) = r.attributes[k]?.decodeToString()
        if (attr("v") != "2") return
        val id = attr("id") ?: r.serviceName
        if (id == ownId) return
        val addr = (if (Build.VERSION.SDK_INT >= 34) r.hostAddresses.let { l -> l.firstOrNull { it is Inet4Address } ?: l.firstOrNull() } else r.host)
            ?: return
        synchronized(peerOf) { peerOf[r.serviceName] = id }
        j.addLANPeer(id, attr("name") ?: "device", addr.hostAddress?.substringBefore('%') ?: return, r.port)
    }
}
