package kz.yerek.aireply.ui.feature.setup

import android.content.Context
import android.view.ViewGroup
import android.widget.MediaController
import android.widget.VideoView
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Fullscreen
import androidx.compose.material.icons.filled.FullscreenExit
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.compose.ui.window.DialogWindowProvider
import androidx.lifecycle.Lifecycle
import androidx.core.content.edit
import androidx.core.net.toUri
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import kz.yerek.aireply.R
import kz.yerek.aireply.core.lang.AppLanguage
import kz.yerek.aireply.ui.LocalServices
import kz.yerek.aireply.ui.common.AppScreen
import kz.yerek.aireply.ui.common.Footnote
import kz.yerek.aireply.ui.common.rememberKeyboardStatus
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing
import kz.yerek.aireply.ui.design.AppCard
import kz.yerek.aireply.ui.design.AppSection
import kz.yerek.aireply.ui.design.StepRow

/** A bundled recording in the app's resolved language, independent of the keyboard layout. */
internal object KeyboardTutorialAssets {
    fun resource(language: AppLanguage): Int = when (language) {
        AppLanguage.ENGLISH -> R.raw.keyboard_setup_en
        AppLanguage.RUSSIAN -> R.raw.keyboard_setup_ru
        AppLanguage.KAZAKH -> R.raw.keyboard_setup_kk
        AppLanguage.UZBEK -> R.raw.keyboard_setup_uz
    }

    fun assetName(language: AppLanguage): String = "keyboard_setup_${language.code}.mp4"
}

/** Optional on first run and permanently available from Settings and Keyboard Setup. */
@Composable
fun KeyboardTutorialEntry() {
    var showing by rememberSaveable { mutableStateOf(false) }
    SecondaryButton(text = stringResource(R.string.tutorial_watch), onClick = { showing = true })
    if (showing) {
        Dialog(
            onDismissRequest = { showing = false },
            properties = DialogProperties(usePlatformDefaultWidth = true, decorFitsSystemWindows = false)
        ) {
            // Use native window bounds so the player and controls stay inside the screen.
            val window = (LocalView.current.parent as? DialogWindowProvider)?.window
            SideEffect {
                window?.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
            }
            KeyboardTutorialScreen(onBack = { showing = false })
        }
    }
}

/** Native offline player; no video, permission prompts or player dependencies enter the IME. */
@Composable
fun KeyboardTutorialScreen(onBack: () -> Unit) {
    val context = LocalContext.current
    val language = LocalServices.current.settings.effectiveAppLanguage
    val resource = KeyboardTutorialAssets.resource(language)
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val progress = remember(context) { context.getSharedPreferences("tutorial_playback", Context.MODE_PRIVATE) }
    val progressKey = "keyboard.${language.code}.position"
    var video by remember(language) { mutableStateOf<VideoView?>(null) }
    var failed by remember(language) { mutableStateOf(false) }
    var expanded by rememberSaveable { mutableStateOf(false) }
    val errorText = stringResource(R.string.tutorial_video_error)

    fun savePosition() {
        val player = video ?: return
        val duration = player.duration
        // A detached VideoView has already released its player and reports zero position.
        if (duration <= 0) return
        val position = player.currentPosition.coerceAtLeast(0)
        val saved = if (position >= duration - 500) 0 else position
        progress.edit { putInt(progressKey, saved) }
    }
    val goBack: () -> Unit = {
        if (expanded) expanded = false else {
            savePosition()
            onBack()
        }
    }
    BackHandler(onBack = goBack)

    DisposableEffect(lifecycle, language) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_PAUSE) {
                savePosition()
                video?.pause()
            }
        }
        lifecycle.addObserver(observer)
        onDispose {
            savePosition()
            lifecycle.removeObserver(observer)
            video?.stopPlayback()
        }
    }

    val playbackControls: @Composable (Modifier) -> Unit = { modifier ->
        Row(modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Spacing.s)) {
            SecondaryButton(
                text = stringResource(R.string.tutorial_play_pause), modifier = Modifier.weight(1f),
                enabled = video != null && !failed,
                onClick = { video?.let { if (it.isPlaying) it.pause() else it.start() } }
            )
            SecondaryButton(
                text = stringResource(R.string.tutorial_replay), modifier = Modifier.weight(1f),
                enabled = video != null && !failed,
                onClick = {
                    progress.edit { putInt(progressKey, 0) }
                    video?.seekTo(0)
                    video?.start()
                }
            )
        }
    }

    AppScreen(
        title = stringResource(R.string.tutorial_keyboard_title),
        onBack = goBack,
        actions = {
            IconButton(onClick = { expanded = !expanded }) {
                Icon(
                    if (expanded) Icons.Default.FullscreenExit else Icons.Default.Fullscreen,
                    contentDescription = stringResource(
                        if (expanded) R.string.tutorial_collapse_video else R.string.tutorial_expand_video
                    )
                )
            }
        },
        scrollable = false,
        bottomBar = if (expanded) {
            { playbackControls(Modifier.padding(horizontal = Spacing.m, vertical = Spacing.s)) }
        } else null
    ) { padding ->
        BoxWithConstraints(Modifier.fillMaxSize().padding(padding)) {
            val playerHeight = (maxHeight * 0.64f).coerceIn(220.dp, 620.dp)
            val scrollState = rememberScrollState()
            Column(
                modifier = Modifier.fillMaxSize()
                    .then(if (expanded) Modifier else Modifier.verticalScroll(scrollState))
                    .padding(Spacing.m),
                verticalArrangement = Arrangement.spacedBy(Spacing.m)
            ) {
                if (!expanded) Footnote(stringResource(R.string.tutorial_offline_body))
                Box(
                    modifier = (if (expanded) Modifier.fillMaxSize() else Modifier.height(playerHeight))
                        .fillMaxWidth().background(androidx.compose.ui.graphics.Color.Black),
                    contentAlignment = Alignment.Center
                ) {
                    if (failed) {
                        Text(errorText, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(Spacing.m))
                    } else {
                        key(resource) {
                            AndroidView(
                                modifier = Modifier.fillMaxSize(),
                                factory = { localized ->
                                    VideoView(localized).apply {
                                        val player = this
                                        val controller = MediaController(localized)
                                        controller.setAnchorView(player)
                                        setMediaController(controller)
                                        setOnPreparedListener {
                                            val saved = progress.getInt(progressKey, 0).coerceIn(0, (duration - 1).coerceAtLeast(0))
                                            seekTo(saved)
                                            if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) start()
                                            controller.show(3_000)
                                        }
                                        setOnCompletionListener { progress.edit { putInt(progressKey, 0) } }
                                        setOnErrorListener { _, _, _ -> failed = true; true }
                                        setVideoURI("android.resource://${localized.packageName}/$resource".toUri())
                                        video = player
                                    }
                                }
                            )
                        }
                    }
                }
                if (!expanded) {
                    playbackControls(Modifier)
                    Footnote(stringResource(R.string.tutorial_language_body))
                    KeyboardTutorialGuide()
                }
            }
        }
    }
}

/** The same steps remain readable while the recording is paused or cannot be opened. */
@Composable
private fun KeyboardTutorialGuide() {
    val context = LocalContext.current
    val status by rememberKeyboardStatus()

    AppSection(stringResource(R.string.tutorial_connect_title)) {
        AppCard {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.m)) {
                StepRow(1, stringResource(R.string.tutorial_step_enable))
                StepRow(2, stringResource(R.string.tutorial_step_select))
            }
        }
        KeyboardSetupAction(status, context)
    }
    AppSection(stringResource(R.string.tutorial_clipboard_title)) {
        AppCard {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.m)) {
                StepRow(3, stringResource(R.string.tutorial_step_copy))
                StepRow(4, stringResource(R.string.tutorial_step_paste, stringResource(R.string.kb_paste_message)))
                Footnote(stringResource(R.string.tutorial_clipboard_body))
            }
        }
    }
    AppSection(stringResource(R.string.tutorial_reply_title)) {
        AppCard {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.m)) {
                StepRow(5, stringResource(R.string.tutorial_step_instruction))
                StepRow(6, stringResource(R.string.tutorial_step_generate,
                    stringResource(R.string.kb_generate), stringResource(R.string.kb_compose_write)))
                StepRow(7, stringResource(R.string.tutorial_step_review))
                StepRow(8, stringResource(R.string.tutorial_step_insert, stringResource(R.string.kb_insert)))
                StepRow(9, stringResource(R.string.tutorial_step_send))
            }
        }
    }
    AppSection(stringResource(R.string.tutorial_voice_title)) {
        AppCard {
            Column(verticalArrangement = Arrangement.spacedBy(Spacing.m)) {
                Text(stringResource(R.string.tutorial_voice_optional), style = MaterialTheme.typography.bodyLarge)
                StepRow(1, stringResource(R.string.tutorial_step_mic))
                StepRow(2, stringResource(R.string.tutorial_step_dictate, stringResource(R.string.voice_rec_done)))
                StepRow(3, stringResource(R.string.tutorial_step_voice_review))
                Footnote(stringResource(R.string.tutorial_voice_privacy))
            }
        }
    }
}
