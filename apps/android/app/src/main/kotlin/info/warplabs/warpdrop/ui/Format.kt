package info.warplabs.warpdrop.ui

import info.warplabs.warpdrop.core.ReceiveState
import info.warplabs.warpdrop.core.ReceiveStatus

private val units = listOf("B", "KB", "MB", "GB", "TB")

fun formatBytes(bytes: Long): String {
    var v = bytes.toDouble()
    var i = 0
    while (v >= 1024 && i < units.lastIndex) { v /= 1024; i++ }
    if (i == 0) return "$bytes B"
    val tenths = (v * 10).toLong()
    val text = if (tenths % 10 == 0L) "${tenths / 10}" else "${tenths / 10}.${tenths % 10}"
    return "$text ${units[i]}"
}

fun filesLabel(n: Int) = if (n == 1) "1 file" else "$n files"

fun percent(done: Long, total: Long): Float = if (total <= 0) 0f else (done.toFloat() / total).coerceIn(0f, 1f)

/** Before the offer arrives; shared by the screen and the notification. */
fun receiveStatusText(s: ReceiveState) = when (s.status) {
    ReceiveStatus.Connecting -> "Connecting…"
    ReceiveStatus.Negotiating -> "Finding the best route…"
    ReceiveStatus.Authenticating -> "Checking the code…"
    ReceiveStatus.Reconnecting -> "Connection dropped, resuming…"
    else -> "Waiting for the sender…"
}

fun formatSpeed(bytesPerSecond: Double) = "${formatBytes(bytesPerSecond.toLong())}/s"

/** "45s", "3m 20s", "1h 5m"; "--" while the speed is unknown. */
fun formatEta(seconds: Double): String {
    if (!seconds.isFinite() || seconds <= 0) return "--"
    val s = kotlin.math.ceil(seconds).toLong()
    return when {
        s < 60 -> "${s}s"
        s < 3600 -> "${s / 60}m ${s % 60}s"
        else -> "${s / 3600}h ${s % 3600 / 60}m"
    }
}

/** Seconds left at [speed], or 0 when the speed is unknown. */
fun eta(remaining: Long, speed: Double) = if (speed > 1) remaining / speed else 0.0

/** Smoothed transfer speed in bytes/s: the web app's SpeedMeter (exponential moving average of samples). */
class SpeedMeter {
    private var t = 0L
    private var bytes = 0L
    var speed = 0.0
        private set

    // shortcut: sampled on progress events, so during a stall (no events) the last speed stays shown; add a ticker if that misleads.
    fun sample(total: Long, nowMs: Long): Double {
        if (t == 0L || total < bytes) {
            t = nowMs; bytes = total
            return speed
        }
        val dt = (nowMs - t) / 1000.0
        if (dt < 0.2 || total == bytes) return speed
        val inst = (total - bytes) / dt
        speed = if (speed == 0.0) inst else speed * 0.7 + inst * 0.3
        t = nowMs; bytes = total
        return speed
    }
}
