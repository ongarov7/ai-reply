package kz.yerek.aireply.ui.feature.setup

import android.content.Context
import android.widget.MediaController
import android.widget.VideoView
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
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
import kz.yerek.aireply.ui.design.SecondaryButton
import kz.yerek.aireply.ui.design.Spacing

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
            properties = DialogProperties(usePlatformDefaultWidth = false)
        ) {
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
    val errorText = stringResource(R.string.tutorial_video_error)

    DisposableEffect(lifecycle, language) {
        fun savePosition() {
            val player = video ?: return
            val position = player.currentPosition.coerceAtLeast(0)
            val saved = if (player.duration > 0 && position >= player.duration - 500) 0 else position
            progress.edit { putInt(progressKey, saved) }
        }
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

    AppScreen(title = stringResource(R.string.tutorial_keyboard_title), onBack = onBack, scrollable = false) { padding ->
        Column(
            modifier = Modifier.fillMaxSize().padding(padding).padding(Spacing.m),
            verticalArrangement = Arrangement.spacedBy(Spacing.s)
        ) {
            Footnote(stringResource(R.string.tutorial_offline_body))
            Box(
                modifier = Modifier.weight(1f).fillMaxWidth().background(androidx.compose.ui.graphics.Color.Black),
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
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Spacing.s)) {
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
    }
}
