package com.letrahoo.monee

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.*
import androidx.compose.material.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.letrahoo.monee.resources.Res
import com.letrahoo.monee.resources.logo
import org.jetbrains.compose.resources.painterResource

@Composable
internal fun BrandHeader() {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Image(painterResource(Res.drawable.logo), "monee Logo", Modifier.size(56.dp))
        Spacer(Modifier.width(12.dp))
        Text("monee", fontSize = 28.sp, fontWeight = FontWeight.Bold, color = Pine)
    }
}
