package info.warplabs.warpdrop

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.IBinder
import androidx.core.app.NotificationCompat
import android.app.NotificationManager
import info.warplabs.warpdrop.core.PeerStatus
import info.warplabs.warpdrop.core.ReceiveState
import info.warplabs.warpdrop.core.SendState
import info.warplabs.warpdrop.ui.filesLabel
import info.warplabs.warpdrop.ui.formatBytes
import info.warplabs.warpdrop.ui.formatSpeed
import info.warplabs.warpdrop.ui.receiveStatusText
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

/**
 * Keeps the process alive and shows progress while a transfer runs, so it survives the screen turning off
 * or the user switching apps. The transfer itself lives in AppController, not here.
 */
class TransferService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var watching = false
    // Wi-Fi power saving batches packets and caps throughput; hold it off while bytes flow (released in onDestroy)
    @Suppress("DEPRECATION") // HIGH_PERF is deprecated in favour of LOW_LATENCY, which only applies with the screen on
    private val wifiLock by lazy {
        getSystemService(WifiManager::class.java).createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "warpdrop:transfer")
            .apply { setReferenceCounted(false) }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    @OptIn(ExperimentalCoroutinesApi::class, FlowPreview::class)
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val controller = (application as WarpApplication).controller
        when (intent?.action) {
            ACTION_STOP -> {
                controller.stopAll()
                stopSelf()
                return START_NOT_STICKY
            }
            ACTION_ACCEPT -> { asking(controller)?.accept(); return START_NOT_STICKY }
            ACTION_DECLINE -> { asking(controller)?.decline(); return START_NOT_STICKY }
        }
        startForeground(NOTIFICATION_ID, notification(Status("Connecting…")), ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        wifiLock.acquire()
        if (!watching) {
            watching = true
            scope.launch {
                val send = controller.send.flatMapLatest { it?.state ?: flowOf(null) }
                val nearby = controller.nearbySends.flatMapLatest { m -> states(m.values.map { it.state }) }
                val receives = controller.receives.flatMapLatest { l -> states(l.map { it.state }) }
                combine(send, nearby, receives, controller.busy) { s, n, r, busy -> Snapshot(s, n, r, busy) }
                    .sample(500)
                    .collect { (s, n, rs, busy) ->
                        if (!busy) { stopSelf(); return@collect }
                        // a decision first, then bytes coming in, then going out
                        val r = rs.firstOrNull { it.awaitingAnswer } ?: rs.firstOrNull { !it.ended }
                        val sends = listOfNotNull(s) + n
                        val status = when {
                            r != null -> receiveStatus(r)
                            sends.any { !it.ended } -> sendStatus(sends.filter { !it.ended })
                            else -> Status("Connecting…")
                        }
                        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(status))
                    }
            }
        }
        return START_NOT_STICKY
    }

    private data class Snapshot(val send: SendState?, val nearby: List<SendState>, val receives: List<ReceiveState>, val busy: Boolean)

    private inline fun <reified T> states(flows: List<StateFlow<T>>): Flow<List<T>> = if (flows.isEmpty()) flowOf(emptyList()) else combine(flows) { it.toList() }

    private fun asking(c: AppController) = c.receives.value.firstOrNull { it.state.value.awaitingAnswer }

    private class Status(val text: String, val title: String? = null, val progress: Int? = null, val upload: Boolean = false, val ask: Boolean = false)

    // Progress shows only once bytes flow; before that the user has not accepted anything yet.
    private fun receiveStatus(r: ReceiveState): Status {
        val offer = r.offer
        return when {
            offer == null -> Status(receiveStatusText(r))
            r.awaitingAnswer -> Status("${filesLabel(offer.size)} · ${formatBytes(r.total)}. Tap to review.", title = "${r.sender.ifBlank { "Someone" }} wants to share", ask = true)
            else -> Status(
                "${formatBytes(r.receivedBytes)} of ${formatBytes(r.total)} · ${formatSpeed(r.speed)}",
                title = "Receiving from ${r.sender.ifBlank { "sender" }}",
                progress = if (r.total > 0) (r.receivedBytes * 1000 / r.total).toInt() else 0,
            )
        }
    }

    private fun sendStatus(live: List<SendState>): Status {
        val s = live.first()
        val active = live.flatMap { it.receivers }.filter { it.status == PeerStatus.Sending }
        if (active.isEmpty()) {
            val to = live.mapNotNull { it.nearbyTo }
            if (to.isNotEmpty()) return Status("Waiting for ${to.joinToString()} to accept", title = "Sharing ${filesLabel(s.files.size)}", upload = true)
            val room = s.room ?: return Status(s.message.ifBlank { "Creating your code…" }, upload = true)
            return Status("Code ${room.code}", title = "Sharing ${filesLabel(s.files.size)}", upload = true)
        }
        val sent = active.sumOf { it.sent }
        val total = active.sumOf { it.total }
        return Status(
            "Sending to ${active.joinToString { it.device.ifBlank { "receiver" } }} · ${formatSpeed(active.sumOf { it.speed })}",
            title = "Sharing ${filesLabel(s.files.size)}",
            progress = if (total > 0) (sent * 1000 / total).toInt() else 0,
            upload = true,
        )
    }

    override fun onDestroy() {
        if (wifiLock.isHeld) wifiLock.release()
        scope.cancel()
        super.onDestroy()
    }

    private fun notification(st: Status): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
        fun action(code: Int, action: String) = PendingIntent.getService(this, code, Intent(this, TransferService::class.java).setAction(action), PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(if (st.upload) android.R.drawable.stat_sys_upload else android.R.drawable.stat_sys_download)
            .setContentTitle(st.title ?: getString(R.string.app_name))
            .setContentText(st.text)
            .setContentIntent(open)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .apply {
                if (st.progress != null) setProgress(1000, st.progress, false)
                if (st.ask) {
                    addAction(0, getString(R.string.decline), action(2, ACTION_DECLINE))
                    addAction(0, getString(R.string.accept), action(3, ACTION_ACCEPT))
                } else {
                    addAction(0, getString(R.string.stop), action(1, ACTION_STOP))
                }
            }
            .build()
    }

    companion object {
        const val CHANNEL = "transfers"
        private const val NOTIFICATION_ID = 1
        private const val ACTION_STOP = "info.warplabs.warpdrop.STOP"
        private const val ACTION_ACCEPT = "info.warplabs.warpdrop.ACCEPT"
        private const val ACTION_DECLINE = "info.warplabs.warpdrop.DECLINE"
    }
}
