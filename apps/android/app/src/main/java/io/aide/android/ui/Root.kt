package io.aide.android.ui

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.togetherWith
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.fadeOut
import androidx.compose.animation.fadeIn
import androidx.compose.animation.core.VisibilityThreshold
import androidx.compose.animation.AnimatedContent
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Settings
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHostState
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.graphics.vector.rememberVectorPainter
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.sp
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import io.aide.android.connection.Link
import io.aide.android.model.AppModel

/** A page over the tabs. */
sealed interface Route {
    data class Session(val id: String) : Route
    /** One of a coding agent's own sessions, from its bot's page (工单 #76): DSH's, the Claude desktop app's. */
    data class DSHSession(val entry: io.aide.android.api.DSHSessionEntry) : Route
    data class ClaudeSession(val entry: io.aide.android.api.ClaudeSessionEntry) : Route
    /** A contact, from its bot's page (docs/personas.md); an empty one adds. */
    data class EditContact(val persona: io.aide.android.api.Persona) : Route
    /** A bot's own page (工单 #57). */
    data class Bot(val key: String) : Route
    /** A bot's settings, a page of their own from its page (工单 #97). */
    data class BotSettings(val key: String) : Route
    /** A room of several bots (G2-b/c), and making one. */
    data class Room(val machineId: String, val roomId: String) : Route
    data object NewRoom : Route
}

/** 4 Fluent tabs (工单 #106): Home (overview + needs you + tasks) opens by default, then Bot, Machines, Settings. */
enum class Tab { Home, Bots, Devices, Settings }

/** The page shown over the tabs (none: the tabs), and how deep in the stack, so a transition knows forward from back. */
private data class PageAt(val route: Route?, val depth: Int)

@Composable
fun Root(model: AppModel) {
    val phase by model.phase.collectAsState()
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(Unit) { model.messages.collect { snackbar.showSnackbar(it) } }
    Box(Modifier.fillMaxSize()) {
        when (phase) {
            AppModel.Phase.Starting -> Centered { ActivityIndicator(size = 28.dp) }
            AppModel.Phase.Failed -> Centered { Text(t("Aide could not start: ", "Aide 无法启动：") + model.startError) }
            AppModel.Phase.Onboarding -> Onboarding(model, snackbar)
            AppModel.Phase.Ready -> {
                // The bots' faces read their states from the sessions (工单 #83).
                val world by model.world.collectAsState()
                CompositionLocalProvider(LocalSessions provides world.sessions.values) { Home(model, snackbar) }
                BackgroundGuide(model)
            }
        }
    }
}

@Composable
private fun Home(model: AppModel, snackbar: SnackbarHostState) {
    val world by model.world.collectAsState()
    // Opens on Home (首页) by default per owner's #106 decision (overrides #57).
    var tab by rememberSaveable { mutableStateOf(Tab.Home) }
    val stack = remember { mutableStateListOf<Route>() }
    BackHandler(enabled = stack.isNotEmpty()) { stack.removeAt(stack.lastIndex) }
    // A notification tapped: what it is about.
    val opening by model.opening.collectAsState()
    LaunchedEffect(opening) {
        val o = opening ?: return@LaunchedEffect
        if (o.session.isNotEmpty()) { stack.clear(); stack.add(Route.Session(o.session)) }
        if (o.room.isNotEmpty()) {
            world.rooms.firstOrNull { it.roomId == o.room }?.let { stack.clear(); stack.add(Route.Room(it.hostMachineId, it.roomId)) }
        }
        model.opened()
    }
    AnimatedContent(
        targetState = PageAt(stack.lastOrNull(), stack.size),
        transitionSpec = {
            val forward = targetState.depth > initialState.depth
            val slide = Motion.smooth(IntOffset.VisibilityThreshold)
            val dim = Motion.smooth<Float>()
            if (forward) {
                (slideInHorizontally(slide) { it }).togetherWith(slideOutHorizontally(slide) { -it / 3 } + fadeOut(dim, targetAlpha = 0.6f))
                    .apply { targetContentZIndex = 1f }
            } else {
                (slideInHorizontally(slide) { -it / 3 } + fadeIn(dim, initialAlpha = 0.6f)).togetherWith(slideOutHorizontally(slide) { it })
                    .apply { targetContentZIndex = -1f }
            }
        },
        contentKey = { it.depth to it.route },
        label = "page",
    ) { at ->
    val top = at.route
    if (top != null) {
        when (top) {
            is Route.Session -> SessionPage(model, top.id, back = { stack.removeAt(stack.lastIndex) }, open = { stack[stack.lastIndex] = Route.Session(it) })
            is Route.DSHSession -> DSHSessionPage(model, top.entry, back = { stack.removeAt(stack.lastIndex) })
            is Route.ClaudeSession -> ClaudeSessionPage(model, top.entry, back = { stack.removeAt(stack.lastIndex) })
            is Route.Bot -> BotPage(model, top.key, openSession = { stack.add(Route.Session(it)) }, edit = { stack.add(Route.EditContact(it)) }, back = { stack.removeAt(stack.lastIndex) },
                openDevices = { stack.clear(); tab = Tab.Devices }, openSettings = { stack.add(Route.BotSettings(top.key)) },
                openRoom = { stack.add(Route.Room(it.hostMachineId, it.roomId)) },
                openOwn = { e -> stack.add(if (e is OwnEntry.Dsh) Route.DSHSession(e.entry) else Route.ClaudeSession((e as OwnEntry.Claude).entry)) })
            is Route.BotSettings -> BotSettingsPage(model, top.key, edit = { stack.add(Route.EditContact(it)) }, back = { stack.removeAt(stack.lastIndex) })
            is Route.EditContact -> ContactEditPage(model, top.persona) { stack.removeAt(stack.lastIndex) }
            is Route.Room -> RoomPage(model, top.machineId, top.roomId, openSession = { stack.add(Route.Session(it)) }, back = { stack.removeAt(stack.lastIndex) })
            Route.NewRoom -> NewRoomPage(model, opened = { r -> stack.removeAt(stack.lastIndex); stack.add(Route.Room(r.hostMachineId, r.roomId)) }, back = { stack.removeAt(stack.lastIndex) })
        }
        return@AnimatedContent
    }
    val needs = world.buckets.needsYou.size
    Scaffold(
        snackbarHost = { IosSnackbarHost(snackbar) },
        bottomBar = {
            // Fluent bottom navigation bar (工单 #106): 4 tabs (首页 / Bot / 机器 / 设置), acrylic fill, Win11 indicator bar.
            Column(Modifier.background(aide.bar)) {
                Hairline()
                Row(Modifier.fillMaxWidth().windowInsetsPadding(WindowInsets.navigationBars).height(64.dp)) {
                    TabItem(t("Home", "首页"), rememberVectorPainter(Icons.Filled.Home), tab == Tab.Home, badge = needs) { tab = Tab.Home }
                    TabItem("Bot", rememberVectorPainter(Icons.AutoMirrored.Filled.List), tab == Tab.Bots) { tab = Tab.Bots }
                    TabItem(t("Machines", "机器"), androidx.compose.ui.res.painterResource(io.aide.android.R.drawable.ic_devices), tab == Tab.Devices) { tab = Tab.Devices }
                    TabItem(t("Settings", "设置"), rememberVectorPainter(Icons.Filled.Settings), tab == Tab.Settings) { tab = Tab.Settings }
                }
            }
        },
    ) { pad ->
        AnimatedContent(
            targetState = tab,
            modifier = Modifier.padding(pad),
            transitionSpec = {
                fadeIn(tween(Motion.Short, easing = Motion.EaseOut))
                    .togetherWith(fadeOut(tween(110, easing = Motion.EaseOut)))
            },
            label = "tab",
        ) { currentTab ->
            when (currentTab) {
                Tab.Home -> SessionsTab(model) { stack.add(Route.Session(it)) }
                Tab.Bots -> BotsTab(model, openRoom = { stack.add(Route.Room(it.hostMachineId, it.roomId)) }, newRoom = { stack.add(Route.NewRoom) }) { stack.add(Route.Bot(it)) }
                Tab.Devices -> DevicesTab(model) { stack.add(Route.Session(it)) }
                Tab.Settings -> SettingsTab(model)
            }
        }
    }
    }
}

/** One Fluent tab: 24dp icon, 12sp label, yellow #ffb900 attention badge with #201f1e text, animated 18×3dp blue bottom indicator. */
@Composable
private fun androidx.compose.foundation.layout.RowScope.TabItem(label: String, icon: Painter, picked: Boolean, badge: Int = 0, onClick: () -> Unit) {
    val tint by animateColorAsState(if (picked) MaterialTheme.colorScheme.primary else aide.gray, tween(Motion.Short, easing = Motion.EaseOut), label = "tabTint")
    val pillWidth by animateDpAsState(if (picked) 18.dp else 0.dp, Motion.snappy(), label = "tabPillW")
    val pillAlpha by animateFloatAsState(if (picked) 1f else 0f, tween(Motion.Short, easing = Motion.EaseOut), label = "tabPillA")
    Column(
        Modifier.weight(1f).fillMaxHeight().selectable(picked, remember { MutableInteractionSource() }, Highlight(aide.pressed), role = Role.Tab, onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center,
    ) {
        Box {
            Icon(icon, null, Modifier.size(24.dp), tint = tint)
            if (badge > 0) Box(
                Modifier.align(Alignment.TopEnd).offset(x = 10.dp, y = (-5).dp).heightIn(min = 16.dp).widthIn(min = 16.dp).background(aide.orange, CircleShape).padding(horizontal = 4.dp),
                contentAlignment = Alignment.Center,
            ) { Text("$badge", color = aide.onCaution, fontSize = 11.sp, lineHeight = 14.sp, fontWeight = FontWeight.SemiBold) }
        }
        Text(label, color = tint, fontSize = 12.sp, lineHeight = 16.sp, fontWeight = if (picked) FontWeight.SemiBold else FontWeight.Normal, modifier = Modifier.padding(top = 3.dp))
        Box(
            Modifier
                .padding(top = 3.dp)
                .size(pillWidth, 3.dp)
                .graphicsLayer { alpha = pillAlpha }
                .background(aide.accent, CircleShape),
        )
    }
}

@Composable
fun Centered(content: @Composable () -> Unit) {
    Column(Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        content()
    }
}

/** The quiet line under a page's title while what shows is not current. */
@Composable
fun SyncLine(model: AppModel) {
    val world by model.world.collectAsState()
    val link by model.link.collectAsState()
    val text = when {
        !world.watched -> t("Connecting to Aide…", "正在连接 Aide…")
        link == Link.Disconnected -> t("Disconnected: no network", "已断开：没有网络")
        link == Link.Reconnecting -> t("Can’t reach the relay: reconnecting…", "无法连接中继：重连中…")
        world.machines.any { it.syncing } -> t("Updating…", "正在更新…")
        else -> return
    }
    Text(text, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 20.dp, vertical = 4.dp))
}
