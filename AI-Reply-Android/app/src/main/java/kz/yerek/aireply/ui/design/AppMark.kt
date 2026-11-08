package kz.yerek.aireply.ui.design

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kz.yerek.aireply.R

/** The app's icon, as a tile with its corners rounded the way the launcher rounds them. */
@Composable
fun AppMark(size: Dp = 64.dp, modifier: Modifier = Modifier) {
    Image(
        painter = painterResource(R.drawable.app_mark_tile),
        contentDescription = null,
        contentScale = ContentScale.Fit,
        modifier = modifier
            .size(size)
            .clip(RoundedCornerShape(size * 0.225f))
            // The screen already names the app; this mark is decorative.
            .clearAndSetSemantics { }
    )
}
