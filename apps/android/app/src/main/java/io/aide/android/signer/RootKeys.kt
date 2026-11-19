package io.aide.android.signer

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.UserNotAuthenticatedException
import io.aide.android.hermeshcore.Hermeshcore
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PrivateKey
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.util.UUID

/**
 * The owner root keys of this phone, one per profile the owner role keeps
 * (docs/trusted-devices.md 4.8), as the iPhone keeps them in its Secure
 * Enclave: P-256 keys made in the Android Keystore, in StrongBox when the
 * phone has one, else in its TEE, which never let the private key out.
 * Every use needs the person, by a strong biometric or the screen lock,
 * within AUTH_SECONDS before it; keys work only while the phone is
 * unlocked. Signatures are ASN.1 DER ECDSA P-256 over SHA-256(message).
 */
object RootKeys {
    private const val PREFIX = "hermesh-root-"
    private const val STORE = "AndroidKeyStore"

    /** How long one confirmation lets a batch be signed: the time a batch of re-issued grants takes in StrongBox. */
    const val AUTH_SECONDS = 10

    private fun store(): KeyStore = KeyStore.getInstance(STORE).apply { load(null) }

    /** Makes a new root key, keeping the others, and returns its public key (SPKI DER). */
    fun create(): ByteArray {
        val alias = PREFIX + UUID.randomUUID()
        try {
            generate(alias, strongBox = true)
        } catch (e: Exception) {
            // No StrongBox (StrongBoxUnavailableException, at times wrapped): the TEE.
            store().deleteEntry(alias)
            generate(alias, strongBox = false)
        }
        return store().getCertificate(alias).publicKey.encoded
    }

    private fun generate(alias: String, strongBox: Boolean) {
        val spec = KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_SIGN)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setUserAuthenticationRequired(true)
            .setUserAuthenticationParameters(AUTH_SECONDS, KeyProperties.AUTH_BIOMETRIC_STRONG or KeyProperties.AUTH_DEVICE_CREDENTIAL)
            // A new fingerprint must not cost the phone its place among the trusted devices (Face ID's userPresence keeps it too).
            .setInvalidatedByBiometricEnrollment(false)
            .setUnlockedDeviceRequired(true)
            .setIsStrongBoxBacked(strongBox)
            .build()
        KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, STORE).apply { initialize(spec) }.generateKeyPair()
    }

    /** The alias of the key keyID names (the owner role's id: a fingerprint of its public key). */
    private fun alias(keyID: String): String? {
        val ks = store()
        return ks.aliases().toList().filter { it.startsWith(PREFIX) }.firstOrNull { a ->
            runCatching { Hermeshcore.keyID(ks.getCertificate(a).publicKey.encoded) }.getOrNull() == keyID
        }
    }

    /** Thrown when the confirmation's time ran out mid-batch: confirm again and go on. */
    class NeedsConfirmation : Exception("confirm it is you")

    /** The key keyID names, to sign with. */
    fun key(keyID: String): PrivateKey {
        val a = alias(keyID) ?: throw IllegalStateException("This phone does not have that owner key")
        return store().getKey(a, null) as PrivateKey
    }

    /** Signs message with key; NeedsConfirmation when the person's confirmation is too old (or there was none). */
    fun sign(key: PrivateKey, message: ByteArray): ByteArray = try {
        Signature.getInstance("SHA256withECDSA").run {
            initSign(key)
            update(message)
            sign()
        }
    } catch (e: UserNotAuthenticatedException) {
        throw NeedsConfirmation()
    }

    /** Forgets the key keyID names (the phone left that owner). */
    fun delete(keyID: String) {
        alias(keyID)?.let { store().deleteEntry(it) }
    }

    /** Where the key keyID is kept: "StrongBox", "TEE", or "software" (a phone that has neither). */
    fun securityLevel(keyID: String): String? {
        val a = alias(keyID) ?: return null
        val key = store().getKey(a, null) as PrivateKey
        val info = KeyFactory.getInstance(key.algorithm, STORE).getKeySpec(key, KeyInfo::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            return when (info.securityLevel) {
                KeyProperties.SECURITY_LEVEL_STRONGBOX -> "StrongBox"
                KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> "TEE"
                KeyProperties.SECURITY_LEVEL_SOFTWARE -> "software"
                else -> "unknown"
            }
        }
        @Suppress("DEPRECATION")
        return if (info.isInsideSecureHardware) "TEE" else "software"
    }
}
