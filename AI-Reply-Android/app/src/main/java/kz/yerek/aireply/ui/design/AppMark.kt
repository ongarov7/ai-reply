package kz.yerek.aireply.ui.design

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R

/** The same complete owner-supplied artwork used by the launcher and store icon. */
@Composable
fun AppMark(size: Dp = 64.dp, modifier: Modifier = Modifier) {
    Image(
        painter = painterResource(R.drawable.app_mark),
        contentDescription = null,
        contentScale = ContentScale.Fit,
        modifier = modifier
            .size(size)
            // The screen already names the app; this mark is decorative.
            .clearAndSetSemantics { }
    )
}
