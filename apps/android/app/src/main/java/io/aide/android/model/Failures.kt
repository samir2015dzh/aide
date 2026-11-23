package io.aide.android.model

import io.aide.android.ui.t

// A failure in words (工单 #105, as the desktop's failureHeadline / failureAdvice in @aide/core): what happened to which
// agent and what the owner can do, never the agent's raw message, which the page keeps folded under Details. The machine
// sorts failures into kinds and finds the model provider a message names (internal/provider/failure.go); the words are
// here, so each app says them in its own language.

/** Kinds the app has its own words for; any other (unknown, or newer than this app) is not told apart. */
fun toldApart(kind: String): Boolean = kind.isNotEmpty() && kind != "FAILURE_KIND_UNKNOWN" && kind != "FAILURE_KIND_UNSPECIFIED" && headlineOrNull(kind, "", "") != null

/** What happened, in a sentence of the agent ("DSH has no DeepSeek API key yet"); provider: the one the machine found, or "". */
fun failureHeadline(kind: String, agent: String, provider: String = ""): String =
    headlineOrNull(kind, agent, provider) ?: t("$agent ran into an internal error", "$agent 出了个内部错误")

private fun headlineOrNull(kind: String, agent: String, provider: String): String? {
    val p = provider.isNotEmpty()
    return when (kind) {
        "FAILURE_KIND_AUTH" -> t("$agent isn’t signed in", "$agent 未登录")
        "FAILURE_KIND_MISSING_CREDENTIALS" -> if (p) t("$agent has no $provider API key yet", "$agent 还没配 $provider 的密钥") else t("$agent has no API key for its model service", "$agent 还没配模型服务的密钥")
        "FAILURE_KIND_QUOTA" -> if (p) t("$agent is out of $provider quota", "$agent 在 $provider 的额度已用完") else t("$agent is out of quota", "$agent 的额度已用完")
        "FAILURE_KIND_RATE_LIMIT" -> if (p) t("$agent was rate limited by $provider", "$agent 被 $provider 限流了") else t("$agent was rate limited", "$agent 触发了限流")
        "FAILURE_KIND_PROVIDER_ERROR" -> if (p) t("$agent’s $provider service failed", "$agent 用的 $provider 服务出错") else t("$agent’s model service failed", "$agent 的模型服务出错")
        "FAILURE_KIND_NETWORK" -> if (p) t("$agent can’t reach $provider", "$agent 连不上 $provider") else t("$agent lost network connection", "$agent 无法连接网络")
        "FAILURE_KIND_CONTEXT_OVERFLOW" -> t("$agent’s context is full", "$agent 的上下文已满")
        "FAILURE_KIND_MODEL_UNAVAILABLE" -> t("This model isn’t available to $agent", "$agent 无法使用该模型")
        "FAILURE_KIND_INVALID_REQUEST" -> t("$agent’s request was rejected", "$agent 的请求被拒绝")
        "FAILURE_KIND_REFUSED" -> t("The model refused this task", "模型拒绝了该任务")
        "FAILURE_KIND_LIMIT_REACHED" -> t("$agent reached its step limit", "$agent 已达步数上限")
        "FAILURE_KIND_RESUME_REJECTED" -> t("$agent couldn’t resume the conversation", "$agent 无法恢复此前的对话")
        "FAILURE_KIND_AGENT_CRASHED" -> t("$agent quit unexpectedly", "$agent 意外退出了")
        "FAILURE_KIND_STALLED" -> t("$agent made no progress for a long time", "$agent 长时间没有进展")
        "FAILURE_KIND_TIMEOUT" -> t("This turn ran past its time limit", "本轮执行已超时")
        "FAILURE_KIND_START_FAILED" -> t("$agent couldn’t start", "$agent 未能启动")
        "FAILURE_KIND_BASE_REF_UNAVAILABLE" -> t("The worktree’s base ref was not found", "无法创建工作目录：找不到基准引用")
        "FAILURE_KIND_INTERRUPTED" -> t("The turn was stopped", "这一轮被停下了")
        else -> null
    }
}

/** What the owner can do about it, "" when there is nothing of theirs to do; one not told apart: the original is in Details. */
fun failureAdvice(kind: String, agent: String, provider: String = ""): String = when (kind) {
    "FAILURE_KIND_MISSING_CREDENTIALS" -> if (provider.isNotEmpty()) t("Add the $provider API key in $agent’s model settings, then send another message to go on.", "在 $agent 的模型设置里填好 $provider 的密钥，再发一条消息就能接着做。")
    else t("Add the API key in $agent’s model settings, then send another message to go on.", "在 $agent 的模型设置里填好密钥，再发一条消息就能接着做。")
    "FAILURE_KIND_AUTH" -> t("Run the matching CLI once on that machine as the same user to sign in.", "请在该机器上以同一系统用户运行一次对应的 CLI 完成登录。")
    "FAILURE_KIND_QUOTA" -> t("Try again when the quota resets, or switch agents or models.", "请等待额度恢复后再试，或更换 Agent / 模型。")
    "FAILURE_KIND_RATE_LIMIT", "FAILURE_KIND_PROVIDER_ERROR", "FAILURE_KIND_NETWORK" -> t("Temporary issue. Try sending the message again in a moment.", "暂时性问题，请稍后重新发送消息。")
    "FAILURE_KIND_CONTEXT_OVERFLOW", "FAILURE_KIND_RESUME_REJECTED" -> t("Send another message to continue in a fresh context. Your worktree changes are preserved.", "再次发送消息将在新上下文中继续，工作区中的代码改动将会保留。")
    "FAILURE_KIND_MODEL_UNAVAILABLE" -> t("Start a new task with another model.", "请更换模型并开启新任务。")
    "FAILURE_KIND_LIMIT_REACHED" -> t("Send “continue” to keep going.", "发送“继续”即可接着执行。")
    "FAILURE_KIND_AGENT_CRASHED", "FAILURE_KIND_STALLED", "FAILURE_KIND_TIMEOUT" -> t("Send another message to resume the task.", "再次发送消息即可恢复任务。")
    "FAILURE_KIND_START_FAILED" -> t("Open that machine’s page in Aide Settings to check why it can’t start agents.", "请在 Aide 设置中打开该机器的详情页，排查无法启动 Agent 的原因。")
    "FAILURE_KIND_INTERRUPTED" -> ""
    else -> if (toldApart(kind)) "" else t("The original message is in Details.", "详情里有原文。")
}
