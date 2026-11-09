package kz.yerek.aireply.ui.feature.support

import android.text.format.DateFormat
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.LocalTextSelectionColors
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.text.selection.TextSelectionColors
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.outlined.SupportAgent
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kz.yerek.aireply.R
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.openWebPage
import kz.yerek.aireply.ui.design.LocalExtraColors
import kz.yerek.aireply.ui.design.ReadableColumn
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import java.time.Instant
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * «Чат с оператором»: the conversation, newest at the bottom, and a field
 * above the keyboard.
 *
 * Оператормен чат: хабарламалар төменде, жазу өрісі пернетақтаның үстінде.
 *
 * The operator is always «Поддержка AI Reply» and nothing here claims anyone
 * is online. While the screen is started it polls every five seconds; when it
 * stops, polling stops with it.
 */
@Composable
fun SupportChatScreen(onBack: () -> Unit) {
    val services = LocalServices.current
    val chat = services.support
    val state by chat.state.collectAsStateWithLifecycle()
    val account by services.account.state.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current
    val enabled = services.supportOffered

    LaunchedEffect(lifecycle, enabled) {
        if (!enabled) return@LaunchedEffect
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            try {
                chat.open()
                while (true) {
                    delay(SupportChatController.POLL_INTERVAL_MS)
                    chat.poll()
                }
            } finally {
                chat.close()
            }
        }
    }

    AppScreen(title = stringResource(R.string.support_title), onBack = onBack, scrollable = false) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .consumeWindowInsets(padding)
                .imePadding()
        ) {
            Box(modifier = Modifier.weight(1f).fillMaxWidth()) {
                when {
                    !enabled -> Unavailable(account.legalConfig.supportUrl, services.settings.effectiveAppLanguage.code)
                    !state.loaded && state.loadFailed -> LoadFailed(onRetry = chat::retryLoad)
                    !state.loaded -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                    state.isEmpty -> EmptyChat()
                    else -> Messages(state, onRetry = chat::retry, onTop = chat::loadOlder)
                }
            }
            if (enabled) {
                InputBar(draft = chat.draft, loaded = state.loaded, onDraftChange = { chat.draft = it }, onSend = chat::send)
            }
        }
    }
}

@Composable
private fun Messages(state: SupportChatState, onRetry: (String) -> Unit, onTop: suspend () -> Unit) {
    val locale = LocalServices.current.settings.effectiveAppLanguage.locale
    val listState = rememberLazyListState()
    // Newest first, drawn bottom up: the list opens at the latest message and stays there as replies come.
    val reversed = state.messages.asReversed()
    // The name heads a run of the operator's messages, not every bubble.
    val runStarts = remember(state.messages) {
        state.messages.filterIndexed { index, message ->
            message.fromOperator && (index == 0 || !state.messages[index - 1].fromOperator)
        }.map { it.key }.toSet()
    }
    val dayStarts = remember(state.messages) { chatDayStarts(state.messages) }

    LaunchedEffect(listState, state.hasOlder) {
        if (!state.hasOlder) return@LaunchedEffect
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1 }
            .distinctUntilChanged()
            .filter { it >= listState.layoutInfo.totalItemsCount - 1 }
            .collect { onTop() }
    }
    // A new message of ours, or a reply, scrolls into view.
    LaunchedEffect(state.messages.lastOrNull()?.key) {
        if (listState.firstVisibleItemIndex <= 1) listState.animateScrollToItem(0)
    }

    LazyColumn(
        state = listState,
        reverseLayout = true,
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = Spacing.m, vertical = Spacing.s),
        verticalArrangement = Arrangement.spacedBy(Spacing.xs)
    ) {
        items(reversed, key = { it.key }) { message ->
            Column(modifier = Modifier.fillMaxWidth()) {
                dayStarts[message.key]?.let { day -> DaySeparator(day, locale) }
                Bubble(message, showsName = message.key in runStarts, locale = locale, onRetry = onRetry)
            }
        }
        if (state.hasOlder) {
            item(key = "older") {
                Box(Modifier.fillMaxWidth().padding(Spacing.s), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                }
            }
        }
    }
}

/** «Сегодня», or «20 октября», centred above the first message of the day. */
@Composable
private fun DaySeparator(day: LocalDate, locale: Locale) {
    Text(
        if (day == LocalDate.now()) stringResource(R.string.support_today) else formatChatDay(day, locale),
        style = MaterialTheme.typography.labelMedium,
        color = LocalExtraColors.current.textSecondary,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth().padding(vertical = Spacing.xs)
    )
}

@Composable
private fun Bubble(message: ChatMessage, showsName: Boolean, locale: Locale, onRetry: (String) -> Unit) {
    val extras = LocalExtraColors.current
    val mine = !message.fromOperator
    val shape = if (mine) {
        RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp, bottomStart = 18.dp, bottomEnd = 4.dp)
    } else {
        RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp, bottomStart = 4.dp, bottomEnd = 18.dp)
    }
    val operatorName = stringResource(R.string.support_operator)
    val yours = stringResource(R.string.support_your_message)

    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = if (mine) Alignment.End else Alignment.Start
    ) {
        if (showsName) {
            Text(
                operatorName,
                style = MaterialTheme.typography.labelMedium,
                color = extras.textSecondary,
                modifier = Modifier.padding(start = Spacing.s, top = Spacing.xs, bottom = Spacing.xxs)
            )
        }
        Box(
            modifier = Modifier
                .widthIn(max = 320.dp)
                .clip(shape)
                .background(if (mine) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.surface)
                .padding(horizontal = Spacing.s, vertical = Spacing.xs)
                .semantics(mergeDescendants = true) {
                    contentDescription = (if (mine) yours else operatorName) + ": " + message.text
                }
        ) {
            // The theme selects in the accent colour, which is what our own bubble is filled with.
            val selection = if (mine) {
                TextSelectionColors(
                    handleColor = MaterialTheme.colorScheme.onBackground,
                    backgroundColor = MaterialTheme.colorScheme.onPrimary.copy(alpha = 0.4f)
                )
            } else {
                LocalTextSelectionColors.current
            }
            CompositionLocalProvider(LocalTextSelectionColors provides selection) {
                SelectionContainer {
                    Text(
                        message.text,
                        style = MaterialTheme.typography.bodyLarge,
                        color = if (mine) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface
                    )
                }
            }
        }
        val status = when (message.status) {
            ChatMessage.Status.SENDING -> stringResource(R.string.support_sending)
            ChatMessage.Status.FAILED -> if (message.failure == SendFailure.RATE_LIMITED) {
                stringResource(R.string.support_rate_limited)
            } else {
                stringResource(R.string.support_not_sent)
            }
            ChatMessage.Status.SENT -> formatChatTime(message.createdAt, locale).orEmpty()
        }
        Row(
            modifier = Modifier.padding(horizontal = Spacing.xs, vertical = 2.dp),
            horizontalArrangement = Arrangement.spacedBy(Spacing.xs),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Text(
                status,
                style = MaterialTheme.typography.labelSmall,
                color = if (message.status == ChatMessage.Status.FAILED) MaterialTheme.colorScheme.error else extras.textSecondary
            )
            if (message.status == ChatMessage.Status.FAILED && message.clientMessageId != null) {
                Text(
                    stringResource(R.string.support_retry),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier
                        .clip(RoundedCornerShape(6.dp))
                        .clickable { onRetry(message.clientMessageId) }
                        .padding(horizontal = Spacing.xs, vertical = Spacing.xxs)
                )
            }
        }
    }
}

/**
 * The field and Send. What is typed lives on the controller, in memory: it is
 * there again after leaving the screen, but never goes into saved state, which
 * Android keeps outside the app.
 */
@Composable
private fun InputBar(draft: String, loaded: Boolean, onDraftChange: (String) -> Unit, onSend: (String) -> Boolean) {
    var text by remember { mutableStateOf(draft) }
    fun edit(value: String) {
        text = value
        onDraftChange(value)
    }
    val tooLong = SupportChatController.isTooLong(text)
    val canSend = loaded && SupportChatController.isSendable(text)
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.background)
            .padding(horizontal = Spacing.m, vertical = Spacing.xs)
    ) {
        if (tooLong) {
            Text(
                stringResource(R.string.support_too_long, SupportChatController.MAX_LENGTH),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.error,
                modifier = Modifier.padding(bottom = Spacing.xxs)
            )
        }
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(Spacing.xs)) {
            OutlinedTextField(
                value = text,
                onValueChange = ::edit,
                placeholder = { Text(stringResource(R.string.support_input_hint)) },
                maxLines = 5,
                isError = tooLong,
                shape = RoundedCornerShape(22.dp),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedContainerColor = MaterialTheme.colorScheme.surface,
                    unfocusedContainerColor = MaterialTheme.colorScheme.surface
                ),
                modifier = Modifier.weight(1f)
            )
            FilledIconButton(
                onClick = { if (onSend(text)) edit("") },
                enabled = canSend,
                colors = IconButtonDefaults.filledIconButtonColors(),
                modifier = Modifier.padding(bottom = 4.dp).size(48.dp)
            ) {
                Icon(Icons.AutoMirrored.Filled.Send, contentDescription = stringResource(R.string.support_send))
            }
        }
        if (SupportChatController.showsCounter(text)) {
            Text(
                stringResource(R.string.compose_counter, SupportChatController.length(text), SupportChatController.MAX_LENGTH),
                style = MaterialTheme.typography.labelSmall,
                color = if (tooLong) MaterialTheme.colorScheme.error else LocalExtraColors.current.textSecondary,
                modifier = Modifier.padding(top = Spacing.xxs)
            )
        }
    }
}

@Composable
private fun EmptyChat() {
    ReadableColumn(modifier = Modifier.fillMaxSize()) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(top = Spacing.xl),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(Spacing.s)
        ) {
            Box(
                modifier = Modifier
                    .size(56.dp)
                    .clip(CircleShape)
                    .background(MaterialTheme.colorScheme.primaryContainer),
                contentAlignment = Alignment.Center
            ) {
                Icon(
                    Icons.Outlined.SupportAgent,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.size(28.dp)
                )
            }
            Text(stringResource(R.string.support_operator), style = MaterialTheme.typography.titleMedium)
            Text(
                stringResource(R.string.support_empty),
                style = MaterialTheme.typography.bodyMedium,
                color = LocalExtraColors.current.textSecondary,
                textAlign = TextAlign.Center
            )
        }
    }
}

@Composable
private fun Unavailable(supportUrl: String, language: String) {
    val context = LocalContext.current
    ReadableColumn {
        Text(
            stringResource(R.string.support_unavailable),
            style = MaterialTheme.typography.bodyMedium,
            color = LocalExtraColors.current.textSecondary
        )
        if (supportUrl.isNotBlank()) {
            SecondaryButton(stringResource(R.string.support_open_page)) { context.openWebPage(supportUrl, language) }
        }
    }
}

/** The chat could not be read: says so, with the way to ask again. */
@Composable
private fun LoadFailed(onRetry: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxSize().padding(Spacing.l),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(Spacing.xs, Alignment.CenterVertically)
    ) {
        Text(
            stringResource(R.string.support_load_failed),
            style = MaterialTheme.typography.bodyMedium,
            color = LocalExtraColors.current.textSecondary,
            textAlign = TextAlign.Center
        )
        TextButton(onClick = onRetry) { Text(stringResource(R.string.support_retry)) }
    }
}

private fun parseChatTime(raw: String?): Instant? =
    raw?.let { runCatching { OffsetDateTime.parse(it).toInstant() }.getOrNull() }

/** «14:05» on this phone's clock, the day being on its separator; null for an unreadable time. */
internal fun formatChatTime(raw: String?, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): String? {
    val instant = parseChatTime(raw) ?: return null
    val pattern = runCatching { DateFormat.getBestDateTimePattern(locale, "Hm") }.getOrNull() ?: "HH:mm"
    return DateTimeFormatter.ofPattern(pattern, locale).format(instant.atZone(zone))
}

/** «20 октября»: a day other than today. */
internal fun formatChatDay(day: LocalDate, locale: Locale): String {
    val pattern = runCatching { DateFormat.getBestDateTimePattern(locale, "dMMMM") }.getOrNull() ?: "d MMMM"
    return DateTimeFormatter.ofPattern(pattern, locale).format(day)
}

/**
 * The messages, by key, that open a new day on this phone's clock, each with
 * its day; [messages] are oldest first. One not on the server yet belongs to
 * when it was written.
 */
internal fun chatDayStarts(
    messages: List<ChatMessage>,
    zone: ZoneId = ZoneId.systemDefault(),
    today: LocalDate = LocalDate.now(zone)
): Map<String, LocalDate> {
    val starts = LinkedHashMap<String, LocalDate>()
    var previous: LocalDate? = null
    messages.forEach { message ->
        val day = (parseChatTime(message.createdAt) ?: message.writtenAt?.let(Instant::ofEpochMilli))
            ?.atZone(zone)?.toLocalDate() ?: today
        if (day != previous) starts[message.key] = day
        previous = day
    }
    return starts
}
