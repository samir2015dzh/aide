package io.aide.android.signer

import io.aide.android.api.ConnectClient
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import java.util.Base64

/** One request of a batch to sign, as the shared core words it (internal/preview, the Linux and Windows signers' cards). */
@Serializable
data class Card(
    val title: String = "",
    /** A verification code to compare with what the other side shows. */
    val code: String = "",
    val lines: List<String> = emptyList(),
    val why: String = "",
    val warning: String = "",
    /** This phone's own (its setup, its join), signed with its new key. */
    val own: Boolean = false,
)

object Cards {
    fun parse(json: String): List<Card> = ConnectClient.json.decodeFromString(ListSerializer(Card.serializer()), json)

    /** The messages to sign, a JSON array of base64, as the core sends them. */
    fun messages(json: String): List<ByteArray> =
        ConnectClient.json.decodeFromString(ListSerializer(String.serializer()), json).map { Base64.getDecoder().decode(it) }

    /** The signatures, as the core reads them. */
    fun signatures(sigs: List<ByteArray>): String =
        ConnectClient.json.encodeToString(ListSerializer(String.serializer()), sigs.map { Base64.getEncoder().encodeToString(it) })
}

/**
 * What the person already confirmed in the app, so the batch it causes goes
 * straight to the biometric prompt instead of a second confirmation (the
 * iPhone's Expectation): the verification code they compared approving a
 * pairing, or their own setup or join. Anything else shows the cards.
 */
sealed interface Expectation {
    fun matches(cards: List<Card>): Boolean

    data class Code(val code: String) : Expectation {
        override fun matches(cards: List<Card>) = code.isNotEmpty() && cards.any { it.code == code }
    }

    data object Own : Expectation {
        override fun matches(cards: List<Card>) = cards.isNotEmpty() && cards.all { it.own }
    }
}
