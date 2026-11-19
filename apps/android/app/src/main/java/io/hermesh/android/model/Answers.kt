package io.hermesh.android.model

import io.hermesh.android.api.Answer
import io.hermesh.android.api.QuestionItem

/**
 * The answers to an agent's question, from what the person chose for each
 * item (option labels) and typed (an item without options, or "other"), or
 * null while an item that is not optional has neither.
 */
fun buildAnswers(items: List<QuestionItem>, chosen: Map<String, Set<String>>, typed: Map<String, String>): List<Answer>? {
    val out = mutableListOf<Answer>()
    for (item in items) {
        val labels = item.options.map { it.label }
        val selected = labels.filter { it in chosen[item.id].orEmpty() }
        val text = typed[item.id].orEmpty().trim()
        if (selected.isEmpty() && text.isEmpty()) {
            if (item.optional) continue
            return null
        }
        out += Answer(itemId = item.id, selected = if (item.multiSelect) selected else selected.take(1), text = text)
    }
    return out
}

/** A choice tapped: one option of a single-select item replaces the other; a multi-select item's toggles. */
fun choose(item: QuestionItem, current: Set<String>, label: String): Set<String> = when {
    item.multiSelect -> if (label in current) current - label else current + label
    label in current -> emptySet()
    else -> setOf(label)
}
