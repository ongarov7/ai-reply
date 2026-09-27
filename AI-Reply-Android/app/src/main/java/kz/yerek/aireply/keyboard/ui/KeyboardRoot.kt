package kz.yerek.aireply.keyboard.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import kz.yerek.aireply.keyboard.KeyboardKey
import kz.yerek.aireply.keyboard.KeyboardTheme

/**
 * The whole keyboard: the persona row or the composer, then the keys.
 *
 * The central layout guarantee: the key area has ONE height for every page
 * of every enabled layout (ҚАЗ's five rows set it), and the AI area above
 * never takes any of it. When a reply is being written the keyboard grows;
 * typing feels the same in every state.
 */
@Composable
fun KeyboardRoot(
    theme: KeyboardTheme,
    keys: KeySurfaceState,
    controller: KeySurfaceController,
    onAccessibilityKey: (KeyboardKey) -> Unit,
    top: @Composable () -> Unit,
    modifier: Modifier = Modifier
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(theme.background)
            // Above the gesture bar rather than under it.
            .navigationBarsPadding()
    ) {
        top()
        KeySurface(
            state = keys,
            theme = theme,
            controller = controller,
            onAccessibilityKey = onAccessibilityKey
        )
    }
}
