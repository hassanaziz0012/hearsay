package me.hassandev.hearsay

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Log
import android.view.inputmethod.InputMethodManager
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.net.BindException
import java.net.InetSocketAddress
import java.net.Socket
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.Principal
import java.security.PrivateKey
import java.security.SecureRandom
import java.security.cert.X509Certificate
import java.security.spec.ECGenParameterSpec
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLServerSocket
import javax.net.ssl.SSLSocket
import javax.net.ssl.X509ExtendedKeyManager
import javax.net.ssl.X509TrustManager
import javax.security.auth.x500.X500Principal
import kotlin.concurrent.thread

/**
 * The PC's connection to the phone over the local network, run by KeyPressService. A TLS
 * 1.3 server on port 47800 (or any free port), advertised over mDNS as `_hearsay._tcp` with
 * the first 16 hex digits of its certificate's SHA-256 in the TXT record (`fp`).
 *
 * Both sides have a long-lived key and self-signed certificate (here in AndroidKeyStore, so
 * the private key never leaves it) and pin each other's certificate fingerprint after
 * pairing. A connection from an unpaired certificate gets `not_paired` and is closed
 * before anything is read, unless HomeActivity is open, which allows pairing:
 *
 *   PC → {"op":"pair","name":"<computer>","commit":"<hex SHA-256 of the PC's nonce>"}
 *   ←    {"ok":true,"nonce":"<hex phone nonce>"}
 *   PC → {"op":"reveal","nonce":"<hex PC nonce>"}
 *   both show a 6-digit code (pairCode), the user confirms on the phone
 *   ←    {"ok":true}  or  {"ok":false,"error":"refused"}
 *
 * The PC commits to its nonce before it sees the phone's, so someone in the middle can't
 * search for certificates that make both sides show the same code.
 *
 * Paired PCs send one request line and get one reply line, both JSON:
 *
 *   {"op":"start|stop|cancel|toggle","ime":"<IME id>","settle":700,"timeout":8000,"debug":false}
 *   {"op":"status"}  {"op":"forget"}
 *   {"op":"keyboards"} → {"ok":true,"keyboards":[{"id":"<IME id>","label":"Gboard",
 *                         "enabled":true,"mic":true}, …]}  (for setup without adb)
 *
 * Replies: {"ok":true,"state":"listening"} after a start, {"ok":true,"state":"idle",
 * "text":"…"} after a stop or cancel, {"ok":true,"state":"restored"} when an interrupted
 * session was put back instead, {"ok":false,"error":"<code>","msg":"…"} on failure (codes
 * from Session.Failure, plus busy, bad_request). With "debug", a "log" array of timings.
 */
class PhoneServer(private val service: KeyPressService) {

    private val session = Session(service)
    private val busy = ReentrantLock()
    private val nsd = service.getSystemService(NsdManager::class.java)
    private var socket: SSLServerSocket? = null
    // A listener that failed to register stays "in use", so each attempt gets a new one.
    @Volatile
    private var registration: NsdManager.RegistrationListener? = null
    private val identity by lazy { Identity.load() }

    fun start() {
        val ctx = SSLContext.getInstance("TLS")
        ctx.init(arrayOf(identity.keyManager), arrayOf(AcceptAny), null)
        val server = ctx.serverSocketFactory.createServerSocket() as SSLServerSocket
        server.reuseAddress = true
        try {
            server.bind(InetSocketAddress(PORT))
        } catch (e: BindException) {
            server.bind(InetSocketAddress(0))
        }
        server.needClientAuth = true
        server.enabledProtocols = arrayOf("TLSv1.3")
        socket = server
        port = server.localPort
        Log.i(TAG, "listening on $port")
        thread(name = "hearsay-server", isDaemon = true) {
            try {
                while (true) {
                    val client = server.accept() as SSLSocket
                    thread(name = "hearsay-client", isDaemon = true) { handle(client) }
                }
            } catch (e: Exception) {
                Log.i(TAG, "server stopped: $e")
            }
        }
        advertise()
    }

    fun stop() {
        socket?.close()
        unadvertise()
        port = 0
    }

    /** (Re-)registers the mDNS service, e.g. after the local network permission is granted. */
    fun advertise() {
        unadvertise()
        val info = NsdServiceInfo().apply {
            serviceName = "Hearsay on ${Build.MODEL}"
            serviceType = SERVICE_TYPE
            port = PhoneServer.port
            setAttribute("fp", identity.fingerprint.take(16))
        }
        val listener = object : NsdManager.RegistrationListener {
            override fun onServiceRegistered(info: NsdServiceInfo) {
                advertised = true
                Log.i(TAG, "advertised as ${info.serviceName}")
            }
            override fun onRegistrationFailed(info: NsdServiceInfo, code: Int) {
                Log.w(TAG, "mDNS failed: $code")
                registration = null
            }
            override fun onServiceUnregistered(info: NsdServiceInfo) {}
            override fun onUnregistrationFailed(info: NsdServiceInfo, code: Int) {}
        }
        // Android 17 throws without ACCESS_LOCAL_NETWORK; uncaught, it kills the key presser.
        try {
            nsd.registerService(info, NsdManager.PROTOCOL_DNS_SD, listener)
            registration = listener
        } catch (e: SecurityException) {
            Log.w(TAG, "mDNS refused: $e")
        }
    }

    private fun unadvertise() {
        registration?.let { runCatching { nsd.unregisterService(it) } }
        registration = null
        advertised = false
    }

    private fun handle(sock: SSLSocket) = try {
        sock.use {
            it.soTimeout = 10_000
            it.startHandshake()
            val peer = it.session.peerCertificates[0].encoded
            val fp = fingerprint(peer)
            val input = it.inputStream.buffered()
            val out = it.outputStream
            when {
                PairedComputers(service).name(fp) != null -> request(fp, readJson(input), out)
                pairingPrompt != null -> pair(peer, fp, input, out)
                else -> send(out, error("not_paired", "this computer isn't paired with the phone. Run `hearsay pair`"))
            }
        }
    } catch (e: Exception) {
        Log.i(TAG, "connection: $e")
    }

    private fun request(fp: String, req: JSONObject, out: OutputStream) {
        val op = req.optString("op")
        // Registration fails without the local network permission, and nothing tells the
        // app when it is granted from outside (App info, pm grant): try again now.
        // (Not while a registration is still pending.)
        if (!advertised && registration == null) advertise()
        if (op == "status") return send(out, JSONObject().put("ok", true).put("state", session.phase.name.lowercase()))
        if (op == "keyboards") return send(out, keyboards())
        if (!busy.tryLock()) return send(out, error("busy", "still busy with the last press"))
        try {
            session.beginRequest()
            val debug = req.optBoolean("debug")
            val reply = { r: JSONObject -> send(out, if (debug) r.put("log", JSONArray(session.takeTrace())) else r) }
            val ime = req.optString("ime")
            val settle = req.optLong("settle", 700)
            val timeout = req.optLong("timeout", 8000)
            val stop = { paste: Boolean ->
                session.stop(paste, ime, settle, timeout) { text ->
                    reply(if (text == null) ok("restored") else ok("idle").put("text", text))
                }
            }
            val start = {
                session.start(ime)
                reply(ok("listening"))
            }
            try {
                if (op in setOf("start", "stop", "cancel", "toggle") && !ime.contains('/')) {
                    throw Session.Failure("bad_request", "no keyboard in the request")
                }
                when (op) {
                    "start" -> start()
                    "stop" -> stop(true)
                    "cancel" -> stop(false)
                    "toggle" -> when (session.phase) {
                        Session.Phase.LISTENING, Session.Phase.INTERRUPTED -> stop(true)
                        else -> start()
                    }
                    "forget" -> {
                        PairedComputers(service).remove(fp)
                        send(out, JSONObject().put("ok", true))
                    }
                    else -> throw Session.Failure("bad_request", "unknown request $op")
                }
            } catch (e: Session.Failure) {
                reply(error(e.code, e.message.orEmpty()))
            }
        } finally {
            busy.unlock()
        }
    }

    private fun pair(peer: ByteArray, fp: String, input: InputStream, out: OutputStream) {
        val req = readJson(input)
        val commit = req.optString("commit")
        if (req.optString("op") != "pair" || commit.length != 64) {
            return send(out, error("not_paired", "this computer isn't paired with the phone. Run `hearsay pair`"))
        }
        val name = req.optString("name").take(64).ifBlank { "Computer" }
        val nonce = ByteArray(32).also(SecureRandom()::nextBytes)
        send(out, JSONObject().put("ok", true).put("nonce", nonce.hex()))
        val reveal = readJson(input)
        val pcNonce = reveal.optString("nonce").unhex()
        if (reveal.optString("op") != "reveal" || pcNonce == null || sha256(pcNonce).hex() != commit) {
            return send(out, error("bad_request", "pairing nonce doesn't match its commitment"))
        }
        val code = pairCode(identity.certificate.encoded, peer, pcNonce, nonce)
        val prompt = pairingPrompt ?: return send(out, error("refused", "the Hearsay screen was closed"))
        val latch = CountDownLatch(1)
        var accepted = false
        val dismiss = prompt.ask(name, code) { yes -> accepted = yes; latch.countDown() }
        if (!latch.await(60, TimeUnit.SECONDS)) dismiss()
        if (!accepted) return send(out, error("refused", "pairing was refused on the phone"))
        PairedComputers(service).add(fp, name)
        Log.i(TAG, "paired with $name")
        send(out, JSONObject().put("ok", true))
    }

    /** Every installed keyboard, whether it is enabled and whether it may use the mic. */
    private fun keyboards(): JSONObject {
        val imm = service.getSystemService(InputMethodManager::class.java)
        val pm = service.packageManager
        val enabled = imm.enabledInputMethodList.map { it.id }.toSet()
        val list = JSONArray()
        for (info in imm.inputMethodList) {
            list.put(JSONObject()
                .put("id", info.id)
                .put("label", info.loadLabel(pm).toString())
                .put("enabled", info.id in enabled)
                .put("mic", pm.checkPermission(Manifest.permission.RECORD_AUDIO, info.packageName) ==
                    PackageManager.PERMISSION_GRANTED))
        }
        return JSONObject().put("ok", true).put("keyboards", list)
    }

    private fun ok(state: String) = JSONObject().put("ok", true).put("state", state)
    private fun error(code: String, msg: String) = JSONObject().put("ok", false).put("error", code).put("msg", msg)

    private fun send(out: OutputStream, reply: JSONObject) {
        out.write((reply.toString() + "\n").toByteArray())
        out.flush()
    }

    /** Reads one line of JSON, at most 8 KiB. */
    private fun readJson(input: InputStream): JSONObject {
        val line = ByteArrayOutputStream()
        while (true) {
            val b = input.read()
            if (b == -1 || b == '\n'.code) break
            if (line.size() >= 8192) throw IllegalArgumentException("request too long")
            line.write(b)
        }
        return JSONObject(line.toString(Charsets.UTF_8.name()))
    }

    /** Asks the user to confirm a pairing; returns a function that closes the question. */
    fun interface PairingPrompt {
        fun ask(name: String, code: String, answer: (Boolean) -> Unit): () -> Unit
    }

    /** The phone's key pair (in AndroidKeyStore) and self-signed certificate. */
    private class Identity(val key: PrivateKey, val certificate: X509Certificate) {
        val fingerprint = fingerprint(certificate.encoded)

        val keyManager = object : X509ExtendedKeyManager() {
            override fun chooseServerAlias(keyType: String?, issuers: Array<out Principal>?, socket: Socket?) =
                ALIAS.takeIf { keyType?.startsWith("EC") == true }
            override fun getCertificateChain(alias: String?) = arrayOf(certificate)
            override fun getPrivateKey(alias: String?) = key
            override fun getServerAliases(keyType: String?, issuers: Array<out Principal>?) = arrayOf(ALIAS)
            override fun getClientAliases(keyType: String?, issuers: Array<out Principal>?) = null
            override fun chooseClientAlias(keyType: Array<out String>?, issuers: Array<out Principal>?, socket: Socket?) = null
        }

        companion object {
            const val ALIAS = "hearsay-tls"

            fun load(): Identity {
                val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
                if (!ks.containsAlias(ALIAS)) {
                    val gen = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
                    // DIGEST_NONE: the TLS stack hashes the handshake itself and asks the key
                    // to sign the digest. The keystore self-signs the certificate.
                    gen.initialize(KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_SIGN)
                        .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
                        .setDigests(KeyProperties.DIGEST_NONE, KeyProperties.DIGEST_SHA256,
                            KeyProperties.DIGEST_SHA384, KeyProperties.DIGEST_SHA512)
                        .setCertificateSubject(X500Principal("CN=Hearsay phone"))
                        .build())
                    gen.generateKeyPair()
                }
                return Identity(ks.getKey(ALIAS, null) as PrivateKey, ks.getCertificate(ALIAS) as X509Certificate)
            }
        }
    }

    /** The certificate is checked against the paired list after the handshake instead. */
    private object AcceptAny : X509TrustManager {
        override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
        override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
        override fun getAcceptedIssuers() = arrayOf<X509Certificate>()
    }

    companion object {
        private const val TAG = "Hearsay"
        private const val PORT = 47800
        const val SERVICE_TYPE = "_hearsay._tcp"

        /** The port the server listens on, 0 when it isn't running. */
        @Volatile
        var port = 0
            private set

        /** Whether mDNS registration succeeded (it needs the local network permission). */
        @Volatile
        var advertised = false
            private set

        /** Set by HomeActivity while it is showing: only then can a new PC pair. */
        @Volatile
        var pairingPrompt: PairingPrompt? = null

        fun fingerprint(cert: ByteArray) = sha256(cert).hex()

        /**
         * The 6-digit code both sides show while pairing (the same in cmd/hearsay/pair.go):
         * SHA-256 of "hearsay-pair-v1", the SHA-256 of the phone's and then the PC's
         * certificate, the PC's nonce and the phone's nonce; the first 4 bytes as an
         * unsigned big-endian number, modulo 1 000 000.
         */
        fun pairCode(phoneCert: ByteArray, pcCert: ByteArray, pcNonce: ByteArray, phoneNonce: ByteArray): String {
            val h = MessageDigest.getInstance("SHA-256").run {
                update("hearsay-pair-v1".toByteArray())
                update(sha256(phoneCert))
                update(sha256(pcCert))
                update(pcNonce)
                update(phoneNonce)
                digest()
            }
            val n = ((h[0].toLong() and 0xff) shl 24) or ((h[1].toLong() and 0xff) shl 16) or
                ((h[2].toLong() and 0xff) shl 8) or (h[3].toLong() and 0xff)
            return "%06d".format(n % 1_000_000)
        }

        private fun sha256(b: ByteArray) = MessageDigest.getInstance("SHA-256").digest(b)
        private fun ByteArray.hex() = joinToString("") { "%02x".format(it) }
        private fun String.unhex(): ByteArray? =
            if (length != 64 || any { it !in "0123456789abcdef" }) null
            else ByteArray(32) { substring(it * 2, it * 2 + 2).toInt(16).toByte() }
    }
}

/** Fingerprints (hex SHA-256 of the certificate) and names of the paired computers. */
class PairedComputers(context: Context) {
    private val prefs = context.getSharedPreferences("paired", Context.MODE_PRIVATE)

    fun name(fp: String): String? = prefs.getString(fp, null)
    fun all(): List<Pair<String, String>> = prefs.all.map { (fp, name) -> fp to name.toString() }.sortedBy { it.second }
    fun add(fp: String, name: String) = prefs.edit().putString(fp, name).apply()
    fun remove(fp: String) = prefs.edit().remove(fp).apply()
}
