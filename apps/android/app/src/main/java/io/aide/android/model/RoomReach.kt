package io.aide.android.model

import io.aide.android.api.RoomMemberCheck
import io.aide.android.ui.t
import java.time.Instant

// Whether the members picked for a room can take their turn (工单 #159), in the desktop's words (roomReach.ts and
// rooms.reach): each member picked is asked of for real, its row saying why it may not; the owner is told of those
// that may not and confirms that set before the room is made.

/** Why a member may not take its turn, in words; "" when it can. */
fun reachProblem(c: RoomMemberCheck, now: Instant): String {
    val may = t("May not take its turn: ", "可能接不了力：")
    return when (c.problem) {
        "", "ROOM_MEMBER_PROBLEM_UNSPECIFIED" -> ""
        "ROOM_MEMBER_PROBLEM_NO_GRANT" -> may + t("its grant hasn't reached the machine keeping the room", "它的授权还没到负责房间的机器")
        "ROOM_MEMBER_PROBLEM_NOT_CONTACT" -> may + t("it is no longer among your contacts", "它已不在你的联系人里")
        "ROOM_MEMBER_PROBLEM_NO_INBOX" -> may + t("its inbox isn't set up (its Aide likely needs updating)", "它的信箱还没开通（它那边的 Aide 多半要更新）")
        "ROOM_MEMBER_PROBLEM_NOT_TAKING" -> instantOrNull(c.lastSeen)?.let { agoText(it, now) }?.let {
            may + t("its inbox is set up, but nothing takes its letters now (last there $it, and nothing wakes it)", "信箱已开通，但现在没有东西替它取信（上次来是 $it，也没有门铃叫醒它）")
        } ?: (may + t("its inbox is set up, but nothing takes its letters now (it reads them only when it checks, and nothing wakes it)", "信箱已开通，但现在没有东西替它取信（它只在自己查信箱时才看到，也没有门铃叫醒它）"))
        "ROOM_MEMBER_PROBLEM_RELAY_UNREACHABLE" -> may + t("the machine keeping the room can't reach the server now", "负责房间的机器现在连不上中继")
        "ROOM_MEMBER_PROBLEM_MACHINE_OFFLINE" -> may + t("its machine didn't answer", "它所在的机器没有应答")
        "ROOM_MEMBER_PROBLEM_MACHINE_OUTDATED" -> t("Couldn't check: Aide on its machine is too old to ask; update it", "没法确认：它所在机器上的 Aide 太旧，先更新")
        "ROOM_MEMBER_PROBLEM_AGENT_MISSING" -> may + t("it isn't on its machine now", "它所在的机器上现在找不到它")
        "ROOM_MEMBER_PROBLEM_AGENT_SIGNED_OUT" -> may + t("it is signed out on its machine", "它在那台机器上没登录")
        "ROOM_MEMBER_PROBLEM_AGENT_UNCONFIRMED" -> may + t("its sign-in on its machine is unconfirmed", "它在那台机器上的登录状态未确认")
        "ROOM_MEMBER_PROBLEM_AGENT_PAUSED" -> may + t("you paused it, or its machine", "你暂停了它（或它所在的机器）")
        else -> t("May not take its turn", "可能接不了力")
    }
}

/** The members that may not take their turn, as one key: another set (picked, dropped, checked otherwise) asks again. */
fun unreachableKey(checks: List<RoomMemberCheck>, now: Instant = Instant.now()): String =
    checks.filter { reachProblem(it, now).isNotEmpty() }.map { it.botKey }.sorted().joinToString(" ")
