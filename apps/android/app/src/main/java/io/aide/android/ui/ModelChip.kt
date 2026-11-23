package io.aide.android.ui

import androidx.compose.runtime.Composable
import io.aide.android.api.AgentStatus
import io.aide.android.model.agentTitle
import io.aide.android.model.defaultModelChoice
import io.aide.android.model.modelChoices

/**
 * The composer's model choice (工单 #98, #99), as the desktop's: each model its specific one ("Opus 5.5 · claude-opus-5-5",
 * the chip "Opus 5.5"), the default naming the one it runs. An ACP agent (DSH) says its models only in a session: until
 * its first here (ranHere), the list says so; one whose sessions offer none says that. Nothing to show: no chip.
 */
@Composable
fun ModelChip(agent: String, a: AgentStatus?, value: String, ranHere: Boolean, onChange: (String) -> Unit) {
    if (a == null) return
    val later = if (a.models.isEmpty() && agent != "claude" && agent != "codex") {
        val name = agentTitle(agent)
        if (ranHere) t("$name's sessions offer no choice of model: it runs as its own settings say", "$name 的会话没有提供模型选项：按它自己的设置运行")
        else t("$name names its models only in a session: send the first message with the default model, then its models on this machine are listed here", "$name 只在会话里告诉可用的模型：先用默认模型发出第一条，之后这里列出它在这台机器上的实际模型")
    } else null
    if (a.models.isEmpty() && later == null) return
    val def = defaultModelChoice(a)
    val choices = modelChoices(agent, a.models, value)
    val defTitle = def?.let { t("Default · ${it.title}", "默认 · ${it.title}") } ?: t("Default model", "默认模型")
    ChipPicker(
        t("Model", "模型"), value,
        listOf("" to (if (def?.detail?.isNotEmpty() == true) "$defTitle · ${def.detail}" else defTitle)) + choices.map { it.value to it.label() },
        notes = listOfNotNull(later),
        chip = { v -> if (v.isEmpty()) defTitle else choices.firstOrNull { it.value == v }?.title },
        onChange = onChange,
    )
}
