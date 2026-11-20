package io.aide.android.model

import io.aide.android.ui.t

/** The fewest characters a password has (relay.MinPassword). */
const val MIN_PASSWORD = 8

/** An account as the server compares it: lowercase and trimmed (relay.NormalizeAccount). */
fun normalizeAccount(s: String): String = s.trim().lowercase()

/**
 * What is wrong with a new account and password, as the server would say
 * it (relay.CheckNewLogin), before anything is sent; null when nothing is.
 */
fun newLoginProblem(account: String, password: String): String? {
    val a = normalizeAccount(account)
    return when {
        a.isEmpty() -> t("Type an account name.", "请输入账号。")
        a.length > 64 || a.any { it == ' ' || it == '\t' || it == '\r' || it == '\n' || it == '/' } ->
            t("The account is at most 64 characters, without spaces or slashes.", "账号最多 64 个字符，不能有空格或斜杠。")
        password.codePointCount(0, password.length) < MIN_PASSWORD ->
            t("The password needs at least $MIN_PASSWORD characters.", "密码至少要 $MIN_PASSWORD 个字符。")
        password.trim().equals(a, ignoreCase = true) -> t("The password can’t be the account name.", "密码不能和账号相同。")
        else -> null
    }
}

/** A server typed without TLS (http://, or a bare IP address, which serverURL gives http): the password would cross the network readable. */
fun plainHttp(typed: String): Boolean = serverURL(typed)?.startsWith("http://") == true
