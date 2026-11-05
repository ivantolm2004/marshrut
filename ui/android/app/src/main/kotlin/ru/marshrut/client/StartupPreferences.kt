package ru.marshrut.client

import android.content.Context
import java.io.IOException

object StartupPreferences {
    private fun prefs(context: Context) = context.getSharedPreferences("marshrut.startup.v1", Context.MODE_PRIVATE)
    fun enabled(context: Context) = prefs(context).getBoolean("enabled", true)
    fun wanted(context: Context) = prefs(context).getBoolean("wanted", false)
    fun setEnabled(context: Context, enabled: Boolean) {
        if (!prefs(context).edit().putBoolean("enabled", enabled).commit()) throw IOException("Не удалось сохранить автозапуск")
    }
    fun setWanted(context: Context, wanted: Boolean) {
        if (!prefs(context).edit().putBoolean("wanted", wanted).commit()) throw IOException("Не удалось сохранить состояние VPN")
    }
}
