package io.aide.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.aide.android.api.QueryWorkResponse
import io.aide.android.model.AppModel
import io.aide.android.model.UsageGroup
import io.aide.android.model.UsagePart
import io.aide.android.model.World
import io.aide.android.model.agentTitle
import io.aide.android.model.tokensText
import io.aide.android.model.usageGroups
import io.aide.android.model.usageParts
import io.aide.android.model.usageRequest
import io.aide.android.model.usageTotals
import java.time.LocalDate
import kotlin.math.roundToInt

/** The grouping's name on its control, as the desktop's (按编码 Agent | 按机器). */
fun usageGroupName(g: UsageGroup): String = when (g) {
    UsageGroup.Agent -> t("By coding agent", "按编码 Agent")
    UsageGroup.Machine -> t("By machine", "按机器")
}

/** A part by its name, never its id (工单 #139): a machine since removed says so. */
fun usagePartName(p: UsagePart, group: UsageGroup, world: World): String = when {
    p.other -> t("Other", "其他")
    group == UsageGroup.Agent -> agentTitle(p.id)
    else -> world.machines.firstOrNull { it.machineId == p.id }?.machineName ?: t("Removed machine", "已移除的机器")
}

/**
 * The Home tab's usage (工单 #142): the past year's tokens and today's, then the year split by coding agent or by
 * machine, each part's tokens and share; the parts add up to the year's number above them. Asked again each day and
 * as the counts move; by agent to start with, as the desktop's.
 */
@Composable
fun UsageCard(model: AppModel) {
    val world by model.world.collectAsState()
    val today = LocalDate.now()
    var res by remember { mutableStateOf<QueryWorkResponse?>(null) }
    LaunchedEffect(today, world.workVersion, world.watched) {
        model.queryWork(usageRequest(today))?.let { res = it }
    }
    var group by rememberSaveable { mutableStateOf(usageGroups.first()) }
    val totals = remember(res, today) { usageTotals(res, today) }
    val parts = remember(res, today, group) { usageParts(res, today, group) }
    Column {
        SectionHeader(t("Usage", "用量"))
        Column(Modifier.fillMaxWidth().aideCard().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.Bottom) {
                Column(Modifier.weight(1f)) {
                    Text(t("Past year · tokens", "近一年 · tokens"), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(tokensText(totals.total), style = IosType.Title1)
                }
                Column(horizontalAlignment = Alignment.End) {
                    Text(t("Today", "今天"), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(tokensText(totals.today), style = IosType.Headline)
                }
            }
            if (totals.total > 0) {
                Segmented(usageGroups.map { it to usageGroupName(it) }, group, Modifier.fillMaxWidth()) { group = it }
                parts.forEach { p -> UsagePartRow(p, group, world) }
                if (group == UsageGroup.Machine) {
                    Text(
                        t(
                            "A personal agent follows the account, not a machine: what it asks for is counted on the machine whose coding agent did the work",
                            "个人 Agent 跟着账号走，不属于哪台机器：它派的活算在实际干活的编码 Agent 所在的机器上",
                        ),
                        style = IosType.Caption, color = aide.faint,
                    )
                }
            } else if (res != null) {
                Text(t("Once you start, the days light up here one by one.", "开始用之后，这里会一天天点亮。"), style = IosType.Footnote, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** One part: its face (an agent's) or a mark, its name, its tokens and its share of the year. */
@Composable
private fun UsagePartRow(p: UsagePart, group: UsageGroup, world: World) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        if (group == UsageGroup.Agent && !p.other) {
            AgentFace(p.id, 20)
        } else {
            Box(Modifier.padding(6.dp).size(8.dp).background(if (p.other) aide.disabled else aide.faint, RoundedCornerShape(2.dp)))
        }
        HSpace(8)
        Text(usagePartName(p, group, world), style = IosType.Subheadline, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
        HSpace(8)
        Text(tokensText(p.tokens), style = IosType.Subheadline, fontWeight = FontWeight.SemiBold)
        HSpace(6)
        Text("${(p.share * 100).roundToInt()}%", style = IosType.Caption, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
