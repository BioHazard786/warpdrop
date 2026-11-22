package info.warplabs.warpdrop

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import android.system.ErrnoException
import android.system.Os
import android.system.OsConstants
import android.webkit.MimeTypeMap
import info.warplabs.warpdrop.core.*
import info.warplabs.warpdrop.ui.SpeedMeter
import info.warplabs.warpdrop.core.mobile.Mobile
import info.warplabs.warpdrop.core.mobile.Offer
import info.warplabs.warpdrop.core.mobile.ReceiveListener
import info.warplabs.warpdrop.core.mobile.SendListener
import info.warplabs.warpdrop.core.mobile.Sink
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import java.io.File
import java.util.UUID
import info.warplabs.warpdrop.core.mobile.NearbyJob as GoNearbyJob
import info.warplabs.warpdrop.core.mobile.ReceiveJob as GoReceiveJob
import info.warplabs.warpdrop.core.mobile.SendJob as GoSendJob

/** A file chosen through the system picker or the share sheet. */
class AndroidFile(val uri: Uri, override val name: String, override val size: Long, override val mime: String) : PickedFile

/** The transfer engine on Android: the Go core from warpcore.aar, files handed over as file descriptors. */
class GoWarpCore(private val context: Context) : WarpCore {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    init {
        Mobile.touch() // loads libgojni
        Mobile.setAndroidSDK(Build.VERSION.SDK_INT) // lets pion list network interfaces on Android 11+
    }

    override fun startSend(files: List<PickedFile>, settings: Settings, deviceName: String): SendSession =
        GoSendSession(context, scope, files, settings, deviceName)

    override fun startReceive(code: String, settings: Settings, deviceName: String): ReceiveSession {
        val job = Mobile.newReceiveJob(settings.server, deviceName, settings.forceRelay)
        return GoReceiveSession(context, job).also { s -> scope.launch { job.start(code, s, s) } }
    }

    override fun startNearby(settings: Settings, deviceName: String, visible: Boolean, onIncoming: (ReceiveSession) -> Unit): NearbySession =
        GoNearbySession(context, scope, settings, deviceName, visible, onIncoming)
}

internal class GoSendSession(
    private val context: Context,
    private val scope: CoroutineScope,
    files: List<PickedFile>,
    settings: Settings,
    deviceName: String,
    /** Send to this device of [nearby] instead of sharing a code. */
    private val nearby: GoNearbyJob? = null,
    private val to: NearbyDevice? = null,
) : SendSession, SendListener {
    private val _state = MutableStateFlow(SendState(files = files, nearbyTo = to?.name))
    override val state: StateFlow<SendState> = _state
    private val job: GoSendJob = Mobile.newSendJob(settings.server, deviceName, settings.forceRelay)

    init {
        scope.launch {
            try {
                for (f in files) job.addFile(openForSend(f as AndroidFile), f.name, f.mime, f.size)
                if (nearby != null && to != null) job.startNearby(nearby, to.id, this@GoSendSession) else job.start(this@GoSendSession)
            } catch (e: Exception) {
                _state.update { it.copy(ended = true, error = "Can't read ${e.message ?: "that file"}") }
            }
        }
    }

    // Go owns the returned fd and closes it. Pipes can't be read at an offset, so those get copied to a temp file first.
    private fun openForSend(f: AndroidFile): Int {
        val pfd = context.contentResolver.openFileDescriptor(f.uri, "r") ?: error(f.name)
        try {
            Os.lseek(pfd.fileDescriptor, 0, OsConstants.SEEK_CUR)
            return pfd.detachFd()
        } catch (e: ErrnoException) {
            if (e.errno != OsConstants.ESPIPE) throw e
        }
        // shortcut: whole file is copied to cache before sending; stream it instead if big pipe-only providers matter
        val tmp = File(context.cacheDir, "send-${UUID.randomUUID()}")
        pfd.use { src -> java.io.FileInputStream(src.fileDescriptor).use { i -> tmp.outputStream().use { o -> i.copyTo(o) } } }
        return android.os.ParcelFileDescriptor.open(tmp, android.os.ParcelFileDescriptor.MODE_READ_ONLY).detachFd()
            .also { tmp.delete() } // the open fd keeps the data readable
    }

    override fun cancel() = job.cancel()

    override fun onRoom(roomID: String, secret: String, link: String) =
        _state.update { it.copy(room = Room(roomID, secret, link)) }

    private val meters = HashMap<String, SpeedMeter>()

    override fun onPeer(id: String, device: String, transport: String, status: String, reason: String, sent: Long, total: Long, filesDone: Int) {
        val speed = synchronized(meters) { meters.getOrPut(id) { SpeedMeter() }.sample(sent, System.currentTimeMillis()) }
        val sas = if (status == "waiting") job.sas(id) else ""
        val r = Receiver(id, device, transport, PeerStatus.parse(status), reason, sent, total, filesDone, speed, sas)
        _state.update { s ->
            val i = s.receivers.indexOfFirst { it.id == id }
            // the code is only known once the handshake is done: keep it through the later states
            val r2 = if (r.sas.isEmpty() && i >= 0) r.copy(sas = s.receivers[i].sas) else r
            s.copy(receivers = if (i < 0) s.receivers + r2 else s.receivers.toMutableList().also { it[i] = r2 })
        }
        // a nearby send has exactly one receiver: once it is over, close the room (after the last frames land)
        if (nearby != null && (status == "done" || status == "failed")) scope.launch { delay(300); job.cancel() }
    }

    override fun onPeerGone(id: String) = _state.update { s ->
        s.copy(receivers = s.receivers.filterNot { it.id == id && it.status != PeerStatus.Done && it.status != PeerStatus.Failed })
    }

    override fun onStatus(msg: String) = _state.update { it.copy(message = msg) }

    override fun onEnd(err: String) = _state.update { it.copy(ended = true, error = err.ifBlank { null }) }
}

/** One receive: through the server ([job]), or a direct one another device started over the local network ([stop]). */
internal class GoReceiveSession(
    private val context: Context,
    private val job: GoReceiveJob?,
    private val stop: (() -> Unit)? = null,
) : ReceiveSession, ReceiveListener, Sink {
    private val _state = MutableStateFlow(ReceiveState())
    override val state: StateFlow<ReceiveState> = _state
    private var answer = CompletableDeferred<Boolean>()
    private val pending = HashMap<Int, Uri>()

    override fun accept() { answer.complete(true) }
    override fun decline() { answer.complete(false) }
    override fun cancel() {
        answer.complete(false)
        job?.cancel()
        stop?.invoke()
    }

    // ---- ReceiveListener (called on Go threads) ----

    override fun onStatus(status: String, transport: String) =
        _state.update { it.copy(status = ReceiveStatus.parse(status), transport = transport.ifBlank { it.transport }) }

    override fun onOffer(offer: Offer): Boolean {
        val files = (0 until offer.count()).map { OfferedFile(offer.name(it), offer.size(it), offer.mime(it)) }
        val before = _state.value.offer
        if (before != null) return before.map { it.name to it.size } == files.map { it.name to it.size } // resume: same list as already accepted
        answer = CompletableDeferred()
        _state.update { it.copy(offer = files, sender = offer.sender(), sas = offer.sas(), awaitingAnswer = true) }
        val ok = runBlocking { answer.await() }
        _state.update { it.copy(awaitingAnswer = false) }
        return ok
    }

    private val meter = SpeedMeter()

    override fun onProgress(idx: Int, bytes: Long) = _state.update {
        val next = it.copy(received = it.received + (idx to bytes))
        next.copy(speed = synchronized(meter) { meter.sample(next.receivedBytes, System.currentTimeMillis()) })
    }

    override fun onFileDone(idx: Int) = _state.update { it.copy(done = it.done + idx) }

    override fun onEnd(err: String) = _state.update { it.copy(ended = true, error = err.ifBlank { null }) }

    // ---- Sink: files are written as pending MediaStore Downloads and published only after the checksum matched ----

    override fun open(name: String, size: Long, idx: Int): Int {
        val ext = name.substringAfterLast('.', "").lowercase()
        val values = ContentValues().apply {
            put(MediaStore.Downloads.DISPLAY_NAME, name)
            put(MediaStore.Downloads.MIME_TYPE, MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream")
            put(MediaStore.Downloads.RELATIVE_PATH, Environment.DIRECTORY_DOWNLOADS)
            put(MediaStore.Downloads.IS_PENDING, 1)
        }
        val resolver = context.contentResolver
        val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) ?: error("cannot create $name in Downloads")
        val pfd = resolver.openFileDescriptor(uri, "w")
        if (pfd == null) {
            resolver.delete(uri, null, null)
            error("cannot open $name for writing")
        }
        synchronized(pending) { pending[idx] = uri }
        return pfd.detachFd()
    }

    override fun finish(idx: Int, ok: Boolean) {
        val uri = synchronized(pending) { pending.remove(idx) } ?: return
        val resolver = context.contentResolver
        if (ok) {
            resolver.update(uri, ContentValues().apply { put(MediaStore.Downloads.IS_PENDING, 0) }, null, null)
            _state.update { it.copy(saved = it.saved + (idx to uri.toString())) }
        } else resolver.delete(uri, null, null)
    }
}
