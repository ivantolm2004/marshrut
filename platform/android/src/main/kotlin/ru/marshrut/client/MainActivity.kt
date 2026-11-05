package ru.marshrut.client

import android.app.Activity
import android.content.Intent
import android.net.VpnService
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import org.json.JSONObject

class MainActivity : FlutterActivity() {
    private var pending: Pair<String, MethodChannel.Result>? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "marshrut/core").setMethodCallHandler { call, result ->
            if (call.method != "request" || call.arguments !is String) {
                result.notImplemented()
                return@setMethodCallHandler
            }
            val request = call.arguments as String
            val action = runCatching { JSONObject(request).optString("action") }.getOrDefault("")
            if (action == "connect") {
                if (pending != null) {
                    result.error("busy", "Ожидается разрешение VPN", null)
                } else {
                    val intent = VpnService.prepare(this)
                    if (intent != null) {
                        pending = request to result
                        startActivityForResult(intent, 4001)
                    } else dispatch(request, result, true)
                }
            } else dispatch(request, result, false)
        }
    }

    private fun dispatch(request: String, result: MethodChannel.Result, connect: Boolean) {
        val action = runCatching { JSONObject(request).optString("action") }.getOrDefault("")
        try {
            if (action == "disconnect") StartupPreferences.setWanted(applicationContext, false)
            if (connect) startForegroundService(Intent(this, TunnelService::class.java))
            Bridge.queries.execute {
                try {
                    ConnectionDiagnostics.initialize(applicationContext)
                    // Loading JNI and opening Keystore must not block the UI thread.
                    Bridge.initialize(applicationContext)
                    if (connect) Bridge.awaitService()
                    if (connect) ConnectionDiagnostics.stage(applicationContext, "connect_request")
                    var response = Bridge.client.request(request)
                    if (connect && JSONObject(response).optBoolean("ok")) StartupPreferences.setWanted(applicationContext, true)
                    if (connect && JSONObject(response).optBoolean("ok")) ConnectionDiagnostics.stage(applicationContext, "connected")
                    if (connect && !JSONObject(response).optBoolean("ok")) Bridge.service?.stopSelf()
                    if (action == "status" || action == "diagnostics") {
                        val parsed = JSONObject(response)
                        if (parsed.optBoolean("ok")) parsed.optJSONObject("data")?.let { data ->
                            val native = ConnectionDiagnostics.snapshot(applicationContext)
                            data.put("android", native).put("core_version", data.optString("version"))
                                .put("version", native.optString("app_version"))
                        }
                        response = parsed.toString()
                    }
                    runOnUiThread { result.success(response) }
                } catch (failure: LinkageError) {
                    ConnectionDiagnostics.failed(applicationContext, failure)
                    if (connect) runCatching { Bridge.service?.stopSelf() }
                    if (action == "diagnostics") diagnosticFallback(result) else nativeFailure(result, failure)
                } catch (failure: Exception) {
                    ConnectionDiagnostics.failed(applicationContext, failure)
                    if (connect) runCatching { Bridge.service?.stopSelf() }
                    if (action == "diagnostics") diagnosticFallback(result)
                    else runOnUiThread { result.error("core", "Сетевое ядро или защищённое хранилище недоступно. Настройки не были перезаписаны.", null) }
                }
            }
        } catch (failure: LinkageError) {
            nativeFailure(result, failure)
        } catch (_: Exception) {
            runOnUiThread { result.error("service", "Не удалось запустить VPN-службу Android", null) }
        }
    }

    private fun diagnosticFallback(result: MethodChannel.Result) {
        val response = JSONObject().put("ok", true).put("data", JSONObject()
            .put("core_unavailable", true).put("android", ConnectionDiagnostics.snapshot(applicationContext))).toString()
        runOnUiThread { result.success(response) }
    }

    private fun nativeFailure(result: MethodChannel.Result, failure: LinkageError) {
        // Raw exception messages may contain paths or credential-bearing data.
        val kind = failure.javaClass.simpleName
        runOnUiThread { result.error("native_load", "Не удалось загрузить сетевое ядро ($kind). Настройки сохранены.", null) }
    }

    @Deprecated("Android VPN permission result")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != 4001) return
        val waiting = pending ?: return
        pending = null
        if (resultCode == Activity.RESULT_OK) dispatch(waiting.first, waiting.second, true)
        else waiting.second.error("permission", "Разрешение на VPN не предоставлено", null)
    }
}
