package ru.marshrut.client

import android.app.ActivityManager
import android.content.Context
import android.os.Build
import org.json.JSONObject
import org.json.JSONArray
import java.io.File
import java.util.concurrent.TimeUnit

/** Only fixed stage codes and exception types are retained, never messages/configs. */
object ConnectionDiagnostics {
    private const val PREFS = "marshrut.connection.diagnostics.v1"
    private var initialized = false
    private var previous = JSONObject()
    private var installedVersion = "unknown"

    @Synchronized fun initialize(context: Context) {
        if (initialized) return
        val preferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        installedVersion = runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "unknown" }.getOrDefault("unknown")
        previous = JSONObject().put("previous_stage", preferences.getString("stage", "unknown"))
            .put("previous_exception", preferences.getString("exception", ""))
            .put("previous_exception_frames", JSONArray(preferences.getString("exception_frames", "[]")))
        var exitPid = 0
        if (Build.VERSION.SDK_INT >= 30) runCatching {
            context.getSystemService(ActivityManager::class.java)
                .getHistoricalProcessExitReasons(context.packageName, 0, 8)
                .firstOrNull { it.processName == context.packageName }?.let {
                    previous.put("exit_reason", it.reason).put("exit_status", it.status)
                        .put("exit_time", it.timestamp)
                        .put("exit_reason_name", when (it.reason) { 4 -> "java_crash"; 5 -> "native_crash"; 6 -> "anr"; 3 -> "low_memory"; else -> "other" })
                    exitPid = it.pid
                }
        }
        // libbox writes native crash output in private storage. Export only
        // known classifications, never raw stack arguments or file contents.
        val crash = File(context.filesDir, "CrashReport-.log")
        if (crash.isFile) runCatching {
            val summary = CrashSummary(readBounded(crash))
            previous.put("native_failure", summary.category).put("native_panic", summary.panicKind)
                .put("native_signal", summary.signal).put("native_symbols", JSONArray(summary.symbols))
                .put("native_file_time", crash.lastModified())
                .put("native_file_matches_exit", previous.has("exit_time") && kotlin.math.abs(crash.lastModified() - previous.optLong("exit_time")) < 30000)
        }
        // Android permits an app to read its own UID's log entries. This is
        // best-effort only, requires no READ_LOGS permission, and exports no messages.
        if (exitPid > 0) readPreviousJavaCrash(context, exitPid)
        val fallback = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, failure ->
            runCatching { failed(context, failure) }
            fallback?.uncaughtException(thread, failure)
        }
        initialized = true
    }

    fun stage(context: Context, code: String) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString("stage", code).commit()
    }
    fun failed(context: Context, failure: Throwable) {
        // Class names cannot contain the exception message, URL or key.
        val types = generateSequence(failure) { it.cause }.take(4)
            .joinToString(" > ") { it.javaClass.name }
        val frames = failure.stackTrace.take(12).map { it.className + "." + it.methodName }
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString("exception", types)
            .putString("exception_frames", JSONArray(frames).toString()).commit()
    }
    private fun readBounded(file: File): String = file.inputStream().use { stream ->
        val bytes = ByteArray(262144)
        var used = 0
        while (used < bytes.size) {
            val count = stream.read(bytes, used, bytes.size - used)
            if (count <= 0) break
            used += count
        }
        String(bytes, 0, used, Charsets.UTF_8)
    }
    private fun readPreviousJavaCrash(context: Context, pid: Int) {
        val temporary = File(context.cacheDir, "previous-java-crash.tmp")
        var process: Process? = null
        try {
            process = ProcessBuilder("/system/bin/logcat", "-b", "crash", "-d", "-v", "raw", "--uid=${android.os.Process.myUid()}", "--pid=$pid", "-t", "256")
                .redirectOutput(temporary).redirectError(File("/dev/null")).start()
            if (!process.waitFor(2, TimeUnit.SECONDS)) return
            val text = readBounded(temporary)
            if (!text.contains("Process: ${context.packageName},")) return
            val summary = CrashSummary(text)
            previous.put("java_log_exception_types", JSONArray(summary.exceptionTypes))
                .put("java_log_symbols", JSONArray(summary.symbols))
        } catch (_: Exception) { /* Available on some firmware only; native file is separate. */ }
        finally { process?.destroy(); temporary.delete() }
    }
    @Synchronized fun snapshot(context: Context): JSONObject {
        initialize(context)
        val preferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        return JSONObject(previous.toString()).put("sdk", Build.VERSION.SDK_INT)
            .put("app_version", installedVersion)
            .put("model", Build.MODEL).put("stage", preferences.getString("stage", "unknown"))
            .put("exception", preferences.getString("exception", ""))
            .put("exception_frames", JSONArray(preferences.getString("exception_frames", "[]")))
    }
}
