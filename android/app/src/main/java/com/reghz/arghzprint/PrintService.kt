package com.reghz.arghzprint

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import mobile.Mobile
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

class PrintService : Service() {
    private var wakeLock: PowerManager.WakeLock? = null
    private var wifiLock: WifiManager.WifiLock? = null
    private val poller = Executors.newSingleThreadScheduledExecutor()
    @Volatile private var started = false

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "Print service", NotificationManager.IMPORTANCE_LOW)
        )
        ServiceCompat.startForeground(
            this, NOTIF_ID, notification("Starting…"),
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE else 0
        )

        wakeLock = getSystemService(PowerManager::class.java)
            .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "arghzprint:service")
            .apply { acquire() }

        val wifiMode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            WifiManager.WIFI_MODE_FULL_LOW_LATENCY
        } else {
            @Suppress("DEPRECATION") WifiManager.WIFI_MODE_FULL_HIGH_PERF
        }
        wifiLock = (applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager)
            .createWifiLock(wifiMode, "arghzprint:wifi")
            .apply { acquire() }

        startCore()
        poller.scheduleWithFixedDelay({ refreshNotification() }, 3, 10, TimeUnit.SECONDS)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int = START_STICKY

    override fun onDestroy() {
        poller.shutdownNow()
        if (started) Mobile.stop()
        wifiLock?.takeIf { it.isHeld }?.release()
        wakeLock?.takeIf { it.isHeld }?.release()
        super.onDestroy()
    }

    private fun startCore() {
        try {
            Mobile.start(filesDir.absolutePath, WebViewRenderer(this))
            started = true
        } catch (e: Exception) {
            Log.e(TAG, "go core failed to start", e)
            notify("Failed to start: ${e.message}")
        }
    }

    private fun refreshNotification() {
        // the core can die after start returned (e.g. UI port taken) — bring it back
        if (!Mobile.running()) {
            startCore()
            return
        }
        val text = try {
            describe(fetchStatus())
        } catch (e: Exception) {
            "Starting…"
        }
        notify(text)
    }

    private fun fetchStatus(): JSONObject {
        val conn = URL("$UI_URL/api/status").openConnection() as HttpURLConnection
        conn.connectTimeout = 2000
        conn.readTimeout = 2000
        return try {
            JSONObject(conn.inputStream.bufferedReader().readText())
        } finally {
            conn.disconnect()
        }
    }

    private fun describe(s: JSONObject): String {
        val parts = mutableListOf(if (s.optBoolean("connected")) "Connected" else "Disconnected")
        val depth = s.optInt("queueDepth")
        if (depth > 0) parts += "$depth waiting"
        val printers = s.optJSONArray("printers")
        if (printers != null) {
            for (i in 0 until printers.length()) {
                val p = printers.getJSONObject(i)
                if (!p.optBoolean("online", true)) parts += "${p.optString("name")} offline"
            }
        }
        return parts.joinToString(" · ")
    }

    private fun notification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentTitle("arghzprint")
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(open)
            .build()
    }

    private fun notify(text: String) {
        getSystemService(NotificationManager::class.java).notify(NOTIF_ID, notification(text))
    }

    companion object {
        const val TAG = "arghzprint"
        // the Go core's web_ui_port must stay 7878 on Android
        const val UI_URL = "http://127.0.0.1:7878"
        private const val CHANNEL_ID = "print_service"
        private const val NOTIF_ID = 1

        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, PrintService::class.java))
        }
    }
}
