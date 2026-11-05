package ru.marshrut.client

import android.net.LocalServerSocket
import android.os.ParcelFileDescriptor
import java.io.File
import java.io.IOException
import java.security.SecureRandom
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Packaged native PIE helper, never an executable from writable app storage.
 * SCM_RIGHTS transfers the actual outgoing socket to VpnService.protect().
 * Passing a child's integer descriptor alone would protect the wrong socket.
 */
class TpwsProcess(private val service: TunnelService) {
    private var child: Process? = null
    private var server: LocalServerSocket? = null
    private val worker = Executors.newSingleThreadExecutor()
    fun start() {
        if (child?.isAlive == true) return
        val file = File(service.applicationInfo.nativeLibraryDir, "libtpws.so")
        if (!file.isFile) throw IOException("tpws не установлен")
        val bytes = ByteArray(16).also { SecureRandom().nextBytes(it) }
        val name = "marshrut-" + bytes.joinToString("") { "%02x".format(it) }
        val listener = LocalServerSocket(name)
        server = listener
        worker.execute {
            while (server === listener) {
                val connection = try { listener.accept() } catch (_: IOException) { break }
                connection.use { socket ->
                    socket.soTimeout = 3000
                    var protected = false
                    try {
                        if (socket.peerCredentials.uid == android.os.Process.myUid() && socket.inputStream.read() == 1) {
                            val descriptors = socket.ancillaryFileDescriptors ?: emptyArray()
                            for (descriptor in descriptors) {
                                ParcelFileDescriptor.dup(descriptor).use { fd -> protected = service.protectSocket(fd.fd) }
                                // Ancillary FileDescriptors must be closed separately after dup.
                                android.system.Os.close(descriptor)
                            }
                            if (descriptors.size != 1) protected = false
                        }
                        socket.outputStream.write(if (protected) 1 else 0)
                    } catch (_: Exception) { /* A failed protect never permits the connection. */ }
                }
            }
        }
        child = ProcessBuilder(file.absolutePath, "--socks", "--bind-addr=127.0.0.1", "--port=2083", "--no-resolve", "--split-pos=1,midsld", "--nosplice")
            .apply { environment()["MARSHRUT_PROTECT_SOCKET"] = name }
            .redirectOutput(File("/dev/null")).redirectError(File("/dev/null")).start()
        Thread.sleep(150)
        if (child?.isAlive != true) { close(); throw IOException("tpws не запустился") }
    }
    fun close() {
        child?.destroy()
        if (child?.waitFor(2, TimeUnit.SECONDS) == false) child?.destroyForcibly()
        child = null
        server?.close()
        server = null
        worker.shutdownNow()
    }
}
