package io.aide.android.ui

import androidx.compose.animation.togetherWith
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.fadeOut
import androidx.compose.animation.fadeIn
import androidx.compose.animation.core.tween
import androidx.compose.animation.Crossfade
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
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
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
    /** A room of several bots (G2-b/c), and making one. */
    data class Room(val machineId: String, val roomId: String) : Route
    data object NewRoom : Route
}

/** The roster first (工单 #57): bots and rooms only; the machines are a tab of their own (工单 #63). */
enum class Tab { Bots, Sessions, NeedsYou, Devices, Settings }

/** The page shown over the tabs (none: the tabs), and how deep in the stack, so a transition knows forward from back. */
private data class PageAt(val route: Route?, val depth: Int)

@Composable
fun Root(model: AppModel) {
    val phase by model.phase.collectAsState()
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(Unit) { model.messages.collect { snackbar.showSnackbar(it) } }
    Box(Modifier.fillMaxSize()) {
        when (phase) {
            AppModel.Phase.Starting -> Centered { CircularProgressIndicator() }
            AppModel.Phase.Failed -> Centered { Text(t("Aide could not start: ", "Aide 无法启动：") + model.startError) }
            AppModel.Phase.Onboarding -> Onboarding(model, snackbar)
            AppModel.Phase.Ready -> {
                Home(model, snackbar)
                BackgroundGuide(model)
            }
        }
    }
}

@Composable
private fun Home(model: AppModel, snackbar: SnackbarHostState) {
    val world by model.world.collectAsState()
    var tab by rememberSaveable { mutableStateOf(Tab.Bots) }
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
    // A page over the tabs slides in a little from the side and fades, back the other way (Motion, as the desktop's).
    AnimatedContent(
        targetState = PageAt(stack.lastOrNull(), stack.size),
        transitionSpec = {
            val forward = targetState.depth > initialState.depth
            val shift = { w: Int -> if (forward) w / 6 else -w / 6 }
            (slideInHorizontally(tween(Motion.Long, easing = Motion.EaseOut), shift) + fadeIn(tween(Motion.Medium, easing = Motion.EaseOut)))
                .togetherWith(slideOutHorizontally(tween(Motion.Long, easing = Motion.EaseOut)) { w -> -shift(w) / 2 } + fadeOut(tween(Motion.Short)))
        },
        contentKey = { it.depth to it.route },
        label = "page",
    ) { at ->
    val top = at.route
    if (top != null) {
        when (top) {
            is Route.Session -> SessionPage(model, top.id, back = { stack.removeAt(stack.lastIndex) })
            is Route.DSHSession -> DSHSessionPage(model, top.entry, back = { stack.removeAt(stack.lastIndex) })
            is Route.ClaudeSession -> ClaudeSessionPage(model, top.entry, back = { stack.removeAt(stack.lastIndex) })
            is Route.Bot -> BotPage(model, top.key, openSession = { stack.add(Route.Session(it)) }, edit = { stack.add(Route.EditContact(it)) }, back = { stack.removeAt(stack.lastIndex) },
                openDevices = { stack.clear(); tab = Tab.Devices },
                openOwn = { e -> stack.add(if (e is OwnEntry.Dsh) Route.DSHSession(e.entry) else Route.ClaudeSession((e as OwnEntry.Claude).entry)) })
            is Route.EditContact -> ContactEditPage(model, top.persona) { stack.removeAt(stack.lastIndex) }
            is Route.Room -> RoomPage(model, top.machineId, top.roomId, openSession = { stack.add(Route.Session(it)) }, back = { stack.removeAt(stack.lastIndex) })
            Route.NewRoom -> NewRoomPage(model, opened = { r -> stack.removeAt(stack.lastIndex); stack.add(Route.Room(r.hostMachineId, r.roomId)) }, back = { stack.removeAt(stack.lastIndex) })
        }
        return@AnimatedContent
    }
    val needs = world.buckets.needsYou.size
    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = {
            // A bar a step off the page with a hairline over it, so the page ends where the bar starts (工单 #54).
            Column {
            androidx.compose.material3.HorizontalDivider(thickness = 0.5.dp, color = aide.separator)
            NavigationBar(containerColor = aide.card, tonalElevation = 0.dp) {
                NavigationBarItem(tab == Tab.Bots, { tab = Tab.Bots }, icon = { Icon(Icons.Filled.Home, null) }, label = { Text("Bot") })
                NavigationBarItem(tab == Tab.Sessions, { tab = Tab.Sessions }, icon = { Icon(Icons.AutoMirrored.Filled.List, null) }, label = { Text(t("Tasks", "任务")) })
                // Needs you: the sessions waiting for an answer, in one place.
                NavigationBarItem(tab == Tab.NeedsYou, { tab = Tab.NeedsYou }, icon = {
                    BadgedBox(badge = { if (needs > 0) Badge(containerColor = Warning, contentColor = androidx.compose.ui.graphics.Color.White) { Text("$needs") } }) { Icon(Icons.Filled.Notifications, null) }
                }, label = { Text(t("Needs you", "需要你")) })
                NavigationBarItem(tab == Tab.Devices, { tab = Tab.Devices }, icon = { Icon(androidx.compose.ui.res.painterResource(io.aide.android.R.drawable.ic_devices), null) }, label = { Text(t("Devices", "设备")) })
                NavigationBarItem(tab == Tab.Settings, { tab = Tab.Settings }, icon = { Icon(Icons.Filled.Settings, null) }, label = { Text(t("Settings", "设置")) })
            }
            }
        },
    ) { pad ->
        Box(Modifier.padding(pad)) {
            Crossfade(tab, animationSpec = tween(Motion.Medium, easing = Motion.EaseOut), label = "tab") { shown ->
                when (shown) {
                    Tab.Sessions -> SessionsTab(model) { stack.add(Route.Session(it)) }
                    Tab.NeedsYou -> NeedsYouTab(model) { stack.add(Route.Session(it)) }
                    Tab.Bots -> BotsTab(model, openRoom = { stack.add(Route.Room(it.hostMachineId, it.roomId)) }, newRoom = { stack.add(Route.NewRoom) }) { stack.add(Route.Bot(it)) }
                    Tab.Devices -> DevicesTab(model) { stack.add(Route.Session(it)) }
                    Tab.Settings -> SettingsTab(model)
                }
            }
        }
    }
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
    Text(text, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
}
