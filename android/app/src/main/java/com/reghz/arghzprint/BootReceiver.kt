package com.reghz.arghzprint

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

// both actions are official exemptions for starting a foreground service from the background
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_BOOT_COMPLETED || intent.action == Intent.ACTION_MY_PACKAGE_REPLACED) {
            PrintService.start(context)
        }
    }
}
