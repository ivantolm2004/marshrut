package ru.marshrut.client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.ParcelFileDescriptor
import ru.marshrut.libbox.*
import java.io.IOException
import java.lang.reflect.Proxy
import java.net.NetworkInterface as JavaInterface
import java.net.InetSocketAddress
import java.util.Collections
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.json.JSONObject

class TunnelService : VpnService() {
    companion object { const val ACTION_RESTORE = "ru.marshrut.client.RESTORE" }
    private val bootRetry = Executors.newSingleThreadScheduledExecutor()
    private var restoreAttempts = 0
    @Volatile private var restoreScheduled = false
    @Volatile private var destroyed = false
    private val lifecycleAccess = Any()
    private var tun: ParcelFileDescriptor? = null
    private var server: CommandServer? = null
    private var tpws: TpwsProcess? = null
    @Volatile private var alive = false
    private var listener: InterfaceUpdateListener? = null
    private var callback: ConnectivityManager.NetworkCallback? = null
    @Volatile private var lastNetwork: Network? = null
    private val connectivity get() = getSystemService(ConnectivityManager::class.java)

    override fun onCreate() {
        super.onCreate()
        val notifications = getSystemService(NotificationManager::class.java)
        notifications.createNotificationChannel(NotificationChannel("vpn", "Подключение Маршрут", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        startForeground(1, Notification.Builder(this, "vpn").setContentTitle("Маршрут")
            .setContentText("Управление подключением").setSmallIcon(android.R.drawable.ic_lock_lock)
            .setContentIntent(open).setOngoing(true).build())
        Bridge.queries.execute {
            try {
                ConnectionDiagnostics.initialize(applicationContext)
                ConnectionDiagnostics.stage(this, "bridge_initialize")
                Bridge.initialize(applicationContext)
                synchronized(lifecycleAccess) { if (!destroyed) Bridge.service = this }
            } catch (failure: Exception) { ConnectionDiagnostics.failed(this, failure); stopSelf() }
              catch (failure: LinkageError) { ConnectionDiagnostics.failed(this, failure); stopSelf() }
        }
    }

    @Synchronized fun startEngine(config: String) {
        if (destroyed) throw IOException("VPN-служба остановлена")
        if (alive) throw IOException("Ядро уже запущено")
        try {
            ConnectionDiagnostics.stage(this, "libbox_setup")
            // libbox's plaintext configuration snapshot is disabled by the build patch.
            Libbox.setup(SetupOptions().apply {
                basePath = filesDir.absolutePath
                workingPath = filesDir.absolutePath
                tempPath = cacheDir.absolutePath
                logMaxLines = 1
                debug = false
                appVersion = "0.1.6"
                appMarketingVersion = "0.1.6"
                // Upstream Android client enables this for Android 9+.
                // Move callback work off the small Android/JNI thread stack.
                fixAndroidStack = true
                oomKillerDisabled = true
                powerReportEnabled = false
            })
            Libbox.checkConfig(config)
            if (config.contains("\"tag\": \"dpi\"")) {
                ConnectionDiagnostics.stage(this, "tpws_start")
                tpws = TpwsProcess(this).also { it.start() }
            }
            val platform = Proxy.newProxyInstance(PlatformInterface::class.java.classLoader, arrayOf(PlatformInterface::class.java)) { proxy, method, args ->
                when (method.name) {
                    "usePlatformAutoDetectInterfaceControl" -> true
                    "autoDetectInterfaceControl" -> {
                        if (!protectSocket(args!![0] as Int)) throw IOException("Не удалось исключить сокет из VPN")
                        null
                    }
                    "openTun" -> openTun(args!![0] as TunOptions)
                    "getInterfaces" -> interfaces()
                    "startDefaultInterfaceMonitor" -> { startMonitor(args!![0] as InterfaceUpdateListener); null }
                    "closeDefaultInterfaceMonitor" -> { stopMonitor(); null }
                    "useProcFS", "underNetworkExtension", "includeAllNetworks", "usePlatformShell", "usePlatformBridge" -> false
                    "readWIFIState" -> Libbox.newWIFIState("", "")
                    "tailscaleHostname" -> "marshrut"
                    "findConnectionOwner" -> findConnectionOwner(args!!)
                    "localDNSTransport", "clearDNSCache", "sendNotification", "cancelNotification", "startNeighborMonitor", "closeNeighborMonitor", "registerMyInterface" -> null
                    "toString" -> "Marshrut Android platform"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.get(0)
                    else -> throw IOException("Функция платформы не поддерживается")
                }
            } as PlatformInterface
            val handler = Proxy.newProxyInstance(CommandServerHandler::class.java.classLoader, arrayOf(CommandServerHandler::class.java)) { proxy, method, args ->
                when (method.name) {
                    "serviceStop" -> { Bridge.operations.execute { StartupPreferences.setWanted(this, false); Bridge.client.request("{\"action\":\"disconnect\"}") }; null }
                    "serviceReload" -> throw IOException("Переподключите приложение")
                    "writeDebugMessage" -> null
                    "getSystemProxyStatus" -> SystemProxyStatus()
                    "setSystemProxyEnabled" -> null
                    "connectSSHAgent" -> -1
                    "toString" -> "Marshrut command handler"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.get(0)
                    else -> throw IOException("Функция не поддерживается")
                }
            } as CommandServerHandler
            server = Libbox.newCommandServer(handler, platform)
            // Do not start libbox's external command listener; Flutter uses the private platform channel.
            ConnectionDiagnostics.stage(this, "core_start")
            server!!.startOrReloadService(config, OverrideOptions())
            if (destroyed) throw IOException("VPN-служба остановлена во время подключения")
            alive = true
            ConnectionDiagnostics.stage(this, "core_ready")
        } catch (failure: Exception) {
            ConnectionDiagnostics.failed(this, failure)
            stopEngine(false)
            throw IOException("Не удалось запустить сетевое ядро Android")
        }
    }

    private fun findConnectionOwner(args: Array<out Any>): ConnectionOwner =
        ConnectionOwnerLookup.resolve(
            args[0] as Int,
            InetSocketAddress(args[1] as String, args[2] as Int),
            InetSocketAddress(args[3] as String, args[4] as Int),
            { protocol, source, destination -> connectivity.getConnectionOwnerUid(protocol, source, destination) },
            { uid ->
                val packages = packageManager.getPackagesForUid(uid)?.toList().orEmpty()
                ConnectionOwner().apply {
                    userId = uid
                    userName = packages.firstOrNull().orEmpty()
                    setAndroidPackageNames(strings(packages))
                }
            })

    private fun openTun(options: TunOptions): Int {
        ConnectionDiagnostics.stage(this, "tun_build")
        val builder = Builder().setSession("Маршрут").setMtu(options.getMTU()).setBlocking(false)
        var iterator = options.getInet4Address()
        while (iterator.hasNext()) { val p = iterator.next(); builder.addAddress(p.address(), p.prefix()) }
        iterator = options.getInet6Address()
        while (iterator.hasNext()) { val p = iterator.next(); builder.addAddress(p.address(), p.prefix()) }
        iterator = options.getInet4RouteRange()
        while (iterator.hasNext()) { val p = iterator.next(); builder.addRoute(p.address(), p.prefix()) }
        iterator = options.getInet6RouteRange()
        while (iterator.hasNext()) { val p = iterator.next(); builder.addRoute(p.address(), p.prefix()) }
        val dns = options.getDNSServerAddress()
        while (dns.hasNext()) builder.addDnsServer(dns.next())
        // A single Android VPN captures both IPv4 and IPv6; IPv6 is not bypassed.
        tun?.close()
        ConnectionDiagnostics.stage(this, "tun_establish")
        tun = builder.establish() ?: throw IOException("Не удалось создать VPN-интерфейс")
        ConnectionDiagnostics.stage(this, "tun_ready")
        return tun!!.fd
    }

    private fun strings(values: List<String>): StringIterator {
        val iterator = values.iterator()
        return object : StringIterator {
            override fun hasNext() = iterator.hasNext()
            override fun next() = iterator.next()
            override fun len() = values.size
        }
    }
    private fun interfaces(): NetworkInterfaceIterator {
        val values = Collections.list(JavaInterface.getNetworkInterfaces()).map { iface ->
            val network = connectivity.allNetworks.firstOrNull { connectivity.getLinkProperties(it)?.interfaceName == iface.name }
            val properties = network?.let { connectivity.getLinkProperties(it) }
            val capabilities = network?.let { connectivity.getNetworkCapabilities(it) }
            NetworkInterface().apply {
                name = iface.name; index = iface.index; setMTU(runCatching { iface.mtu }.getOrDefault(1500))
                addresses = strings(iface.interfaceAddresses.map { "${it.address.hostAddress!!.substringBefore('%')}/${it.networkPrefixLength}" })
                flags = (if (iface.isUp) 1 or 64 else 0) or (if (iface.isLoopback) 8 else 0) or 4096
                type = when { capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true -> Libbox.InterfaceTypeWIFI
                    capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true -> Libbox.InterfaceTypeCellular
                    capabilities?.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) == true -> Libbox.InterfaceTypeEthernet
                    else -> Libbox.InterfaceTypeOther }
                setDNSServer(strings(properties?.dnsServers?.mapNotNull { it.hostAddress } ?: emptyList()))
                gateway = strings(properties?.routes?.mapNotNull { it.gateway?.hostAddress } ?: emptyList())
                metered = capabilities?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED) != true
            }
        }.iterator()
        return object : NetworkInterfaceIterator { override fun hasNext() = values.hasNext(); override fun next() = values.next() }
    }

    private fun preferredNetwork(): Network? {
        val active = connectivity.activeNetwork
        // Networks can disappear between Binder calls while the VPN comes up.
        // Rank the capability snapshot rather than querying it again with !!.
        return connectivity.allNetworks.mapNotNull {
            val c = connectivity.getNetworkCapabilities(it) ?: return@mapNotNull null
            if (!c.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN) || !c.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) return@mapNotNull null
            it to ((if (it == active) 1000 else 0) + (if (c.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)) 100 else 0) +
                (if (c.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) 20 else 0) + (if (c.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) 10 else 0)
            )
        }.maxByOrNull { it.second }?.first
    }
    fun protectSocket(fd: Int): Boolean = try {
        if (!protect(fd)) false else {
            val network = preferredNetwork() ?: throw IOException("Нет физической сети")
            ParcelFileDescriptor.fromFd(fd).use { network.bindSocket(it.fileDescriptor) }
            true
        }
    } catch (_: Exception) { false }
    private fun update() {
        val network = preferredNetwork() ?: return
        val p = connectivity.getLinkProperties(network) ?: return
        val c = connectivity.getNetworkCapabilities(network) ?: return
        if (c.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return
        val name = p.interfaceName ?: return
        val index = JavaInterface.getByName(name)?.index ?: return
        setUnderlyingNetworks(arrayOf(network))
        listener?.updateDefaultInterface(name, index, !c.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED), false)
        val changed = lastNetwork != network
        lastNetwork = network
        if (alive && changed) {
            Bridge.operations.execute {
                try { Bridge.client.request("{\"action\":\"network_changed\"}") }
                catch (failure: Exception) { ConnectionDiagnostics.failed(this, failure) }
            }
        }
    }
    private fun updateSafely() {
        try { update() }
        catch (failure: Exception) { ConnectionDiagnostics.failed(this, failure) }
    }
    private fun startMonitor(next: InterfaceUpdateListener) {
        stopMonitor(); listener = next
        callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) { updateSafely() }
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) { updateSafely() }
            override fun onLinkPropertiesChanged(network: Network, properties: android.net.LinkProperties) { updateSafely() }
            override fun onLost(network: Network) { updateSafely() }
        }
        connectivity.registerNetworkCallback(NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(), callback!!)
        updateSafely()
    }
    private fun stopMonitor() { callback?.let { runCatching { connectivity.unregisterNetworkCallback(it) } }; callback = null; listener = null }
    fun engineAlive() = alive && tun != null
    @Synchronized fun stopEngine(stopService: Boolean = true) {
        alive = false
        runCatching { server?.closeService() }; runCatching { server?.close() }; server = null
        runCatching { tun?.close() }; tun = null
        runCatching { tpws?.close() }; tpws = null
        stopMonitor()
        // A failed boot attempt cleans up the engine but keeps the foreground
        // service available for its bounded retry. Manual disconnect clears wanted.
        if (stopService && !(restoreScheduled && StartupPreferences.enabled(this) && StartupPreferences.wanted(this))) stopSelf()
    }
    override fun onRevoke() { runCatching { StartupPreferences.setWanted(this, false) }; Bridge.operations.execute { Bridge.client.request("{\"action\":\"disconnect\"}") }; super.onRevoke() }
    override fun onDestroy() {
        synchronized(lifecycleAccess) {
            destroyed = true
            if (Bridge.service === this) Bridge.service = null
        }
        bootRetry.shutdownNow()
        // Closing Go/TUN can wait for network workers; never block Android's main thread.
        Bridge.operations.execute { stopEngine(false) }
        super.onDestroy()
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_RESTORE && !restoreScheduled) { restoreScheduled = true; restoreAfterBoot() }
        return START_NOT_STICKY
    }
    private fun restoreAfterBoot() {
        bootRetry.execute {
            try { Bridge.awaitService() } catch (_: Exception) { restoreScheduled = false; stopSelf(); return@execute }
            if (!StartupPreferences.enabled(this) || !StartupPreferences.wanted(this) || VpnService.prepare(this) != null) { restoreScheduled = false; stopSelf(); return@execute }
            if (alive) { restoreScheduled = false; return@execute }
            restoreAttempts++
            val restored = runCatching { JSONObject(Bridge.client.request("{\"action\":\"connect\"}")).optBoolean("ok") }.getOrDefault(false)
            if (!restored) {
                if (restoreAttempts < 6 && !bootRetry.isShutdown) bootRetry.schedule({ restoreAfterBoot() }, 10, TimeUnit.SECONDS)
                else { restoreScheduled = false; stopSelf() }
            } else restoreScheduled = false
        }
    }
}
