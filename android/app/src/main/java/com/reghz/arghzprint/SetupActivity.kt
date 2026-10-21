package com.reghz.arghzprint

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView

object Setup {
    fun notificationsAllowed(ctx: Context) =
        Build.VERSION.SDK_INT < 33 ||
            ctx.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    fun batteryUnrestricted(ctx: Context) =
        ctx.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(ctx.packageName)

    fun isComplete(ctx: Context) = notificationsAllowed(ctx) && batteryUnrestricted(ctx)
}

class SetupActivity : Activity() {
    private lateinit var list: LinearLayout

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        list = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 48, 48, 48)
        }
        setContentView(ScrollView(this).apply { addView(list) })
    }

    // statuses change while the user is away in system settings
    override fun onResume() {
        super.onResume()
        render()
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        render()
    }

    private fun render() {
        list.removeAllViews()

        item(
            "1. Allow notifications",
            "Shows the print service status (connected, jobs waiting, printer offline).",
            Setup.notificationsAllowed(this), "Allow",
        ) {
            if (Build.VERSION.SDK_INT >= 33) requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 1)
        }

        item(
            "2. Turn off battery optimization (required)",
            "Without this, Android may refuse to restart the print service after it gets killed in the background.",
            Setup.batteryUnrestricted(this), "Open",
        ) {
            startActivity(
                Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName"))
            )
        }

        item(
            "3. App settings",
            "If your device has Autostart, Background activity or \"No restrictions\" options, enable them here.",
            null, "Open",
        ) {
            startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
        }

        item(
            "4. Tips",
            "Keep the tablet plugged in. Lock the app in Recent apps if your device supports it. " +
                "If the screen-off still stops printing: Developer options → Stay awake while charging.",
            null, null, null,
        )
    }

    private fun item(title: String, desc: String, ok: Boolean?, action: String?, onClick: (() -> Unit)?) {
        val mark = when (ok) {
            true -> "✅ "
            false -> "⚠️ "
            null -> ""
        }
        list.addView(TextView(this).apply {
            text = mark + title
            textSize = 18f
            setPadding(0, 32, 0, 8)
        })
        list.addView(TextView(this).apply { text = desc })
        if (action != null && onClick != null) {
            list.addView(Button(this).apply {
                text = action
                setOnClickListener { onClick() }
            })
        }
    }
}
