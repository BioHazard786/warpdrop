package info.warplabs.warpdrop.core

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow

/** A file the user chose to send. The platform keeps whatever handle it needs (an Android Uri, a Path). */
interface PickedFile {
    val name: String
    val size: Long
    val mime: String
}

enum class ThemeMode(val label: String) { System("System default"), Light("Light"), Dark("Dark"), Amoled("AMOLED black") }

/** [server] is blank for the hosted default. [dynamicColor] uses the wallpaper palette (Material You) on Android 12+. */
data class Settings(
    val server: String = "",
    val forceRelay: Boolean = false,
    val theme: ThemeMode = ThemeMode.System,
    val dynamicColor: Boolean = true,
    /** Listed to other devices on the network. Off still lets this device see them and send. */
    val nearbyVisible: Boolean = true,
)

interface SettingsStore {
    val settings: Flow<Settings>
    suspend fun save(settings: Settings)
}

/** The transfer engine. The Go core implements it on every platform; the UI only knows this interface. */
interface WarpCore {
    fun startSend(files: List<PickedFile>, settings: Settings, deviceName: String): SendSession
    fun startReceive(code: String, settings: Settings, deviceName: String): ReceiveSession

    /** Joins the same-network device list. [onIncoming] gets every transfer another device starts toward this one. */
    fun startNearby(settings: Settings, deviceName: String, visible: Boolean, onIncoming: (ReceiveSession) -> Unit): NearbySession
}

// ---- nearby ----

/** [direct]: also reachable straight over the local network (works with no internet); otherwise only through the server. */
data class NearbyDevice(val id: String, val name: String, val direct: Boolean)

data class NearbyState(
    val visible: Boolean = true,
    val devices: List<NearbyDevice> = emptyList(),
    /** "" when connected to the server's device list, otherwise why not (the local network still works). */
    val server: String = "",
)

interface NearbySession {
    val state: StateFlow<NearbyState>
    fun setVisible(visible: Boolean)
    /** Sends to a device in [NearbyState.devices]: no code, the receiver accepts first. */
    fun sendTo(files: List<PickedFile>, deviceId: String): SendSession
    fun stop()
}

// ---- send ----

enum class PeerStatus { Connecting, Authenticating, Waiting, Sending, Done, Failed;
    companion object {
        fun parse(s: String) = when (s) {
            "authenticating" -> Authenticating
            "waiting" -> Waiting
            "sending" -> Sending
            "done" -> Done
            "failed" -> Failed
            else -> Connecting
        }
    }
}

data class Receiver(
    val id: String,
    val device: String,
    /** "", "rtc" (direct) or "relay" */
    val transport: String,
    val status: PeerStatus,
    val reason: String,
    val sent: Long,
    val total: Long,
    val filesDone: Int,
    /** Bytes per second, smoothed. */
    val speed: Double = 0.0,
    /** Nearby sends: the 6-digit code the receiver's screen must show too. */
    val sas: String = "",
)

data class Room(val id: String, val secret: String, val link: String) {
    val code get() = "$id-$secret"
}

data class SendState(
    val files: List<PickedFile> = emptyList(),
    val room: Room? = null,
    val receivers: List<Receiver> = emptyList(),
    val message: String = "",
    val ended: Boolean = false,
    val error: String? = null,
    /** The device's name when sending to a nearby device instead of sharing a code. */
    val nearbyTo: String? = null,
)

interface SendSession {
    val state: StateFlow<SendState>
    fun cancel()
}

// ---- receive ----

data class OfferedFile(val name: String, val size: Long, val mime: String)

enum class ReceiveStatus { Connecting, Negotiating, Authenticating, Offered, Receiving, Reconnecting;
    companion object {
        fun parse(s: String) = when (s) {
            "negotiating" -> Negotiating
            "authenticating" -> Authenticating
            "offered" -> Offered
            "receiving" -> Receiving
            "reconnecting" -> Reconnecting
            else -> Connecting
        }
    }
}

data class ReceiveState(
    val status: ReceiveStatus = ReceiveStatus.Connecting,
    val transport: String = "",
    /** Set while the sender's file list waits for accept or decline. */
    val offer: List<OfferedFile>? = null,
    /** The sender's device name, known once the offer arrives. */
    val sender: String = "",
    /** Nearby transfers: the 6-digit code to compare with the sender's screen. */
    val sas: String = "",
    val awaitingAnswer: Boolean = false,
    val received: Map<Int, Long> = emptyMap(),
    val done: Set<Int> = emptySet(),
    /** Where each verified file was saved (a content URI), for previews. */
    val saved: Map<Int, String> = emptyMap(),
    /** Bytes per second, smoothed. */
    val speed: Double = 0.0,
    val ended: Boolean = false,
    val error: String? = null,
) {
    val total get() = offer?.sumOf { it.size } ?: 0L
    val receivedBytes get() = done.sumOf { offer?.getOrNull(it)?.size ?: 0L } +
        received.filterKeys { it !in done }.values.sum()
    /** Finished with every file verified. */
    val succeeded get() = ended && error == null && offer != null && done.size == offer.size
}

interface ReceiveSession {
    val state: StateFlow<ReceiveState>
    fun accept()
    fun decline()
    fun cancel()
}
