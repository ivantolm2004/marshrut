package ru.marshrut.client

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        if (!StartupPreferences.enabled(context) || !StartupPreferences.wanted(context)) return
        // Android requires the user's prior system VPN grant; never bypass it.
        if (VpnService.prepare(context) != null) return
        runCatching { context.startForegroundService(Intent(context, TunnelService::class.java).setAction(TunnelService.ACTION_RESTORE)) }
    }
}
