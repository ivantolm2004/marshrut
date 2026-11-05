package ru.marshrut.client

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import ru.marshrut.mobile.Client
import ru.marshrut.mobile.Host
import ru.marshrut.mobile.Mobile
import java.io.IOException
import java.security.KeyStore
import java.util.concurrent.Executors
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

object Bridge {
    val operations = Executors.newSingleThreadExecutor()
    val queries = Executors.newCachedThreadPool()
    @Volatile var service: TunnelService? = null
    lateinit var client: Client
    private val keyLock = Any()
    @Synchronized fun initialize(context: Context) {
        if (::client.isInitialized) return
        client = Mobile.newClient(context.filesDir.absolutePath, object : Host {
            override fun startupEnabled(): Boolean = StartupPreferences.enabled(context)
            override fun setStartupEnabled(value: Boolean) { StartupPreferences.setEnabled(context, value) }
            override fun startEngine(config: String) { awaitService().startEngine(config) }
            override fun stopEngine() { service?.stopEngine() }
            override fun engineAlive(): Boolean = service?.engineAlive() == true
            override fun dpiAvailable(): Boolean = java.io.File(context.applicationInfo.nativeLibraryDir, "libtpws.so").isFile
            override fun configureDpi(domains: String, excluded: String) { /* The Android SOCKS route isolates tpws from tunnels. */ }
            override fun seal(plain: String): String {
                val cipher = Cipher.getInstance("AES/GCM/NoPadding")
                cipher.init(Cipher.ENCRYPT_MODE, key())
                return Base64.encodeToString(cipher.iv + cipher.doFinal(Base64.decode(plain, Base64.NO_WRAP)), Base64.NO_WRAP)
            }
            override fun unseal(cipher: String): String {
                val bytes = Base64.decode(cipher, Base64.NO_WRAP)
                if (bytes.size < 29) throw IOException("Хранилище повреждено")
                val aes = Cipher.getInstance("AES/GCM/NoPadding")
                aes.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
                return Base64.encodeToString(aes.doFinal(bytes.copyOfRange(12, bytes.size)), Base64.NO_WRAP)
            }
        })
    }
    fun awaitService(): TunnelService {
        repeat(100) { service?.let { return it }; Thread.sleep(50) }
        throw IOException("VPN-служба недоступна")
    }
    private fun key(): SecretKey = synchronized(keyLock) {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val existing = store.getKey("marshrut.state.v1", null) as? SecretKey
        existing ?: KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("marshrut.state.v1", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setKeySize(256).setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
    }
}
