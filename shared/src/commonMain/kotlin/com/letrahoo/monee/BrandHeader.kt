package com.letrahoo.monee

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.Surface
import androidx.compose.ui.graphics.Color
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
        Image(painterResource(Res.drawable.logo), "monee Logo", Modifier.size(40.dp))
        Spacer(Modifier.width(12.dp))
        Text("monee", fontSize = 24.sp, fontWeight = FontWeight.Bold, color = Pine)
    }
}

@Composable
internal fun SettingsPage(content: @Composable ColumnScope.() -> Unit) {
    Box(Modifier.fillMaxSize().background(Color(0xFFF6F7F2)), contentAlignment = Alignment.TopCenter) {
        Column(Modifier.widthIn(max = 1000.dp).fillMaxWidth().verticalScroll(rememberScrollState()).padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp), content = content)
    }
}

@Composable
internal fun SettingsCard(title: String, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier = Modifier.fillMaxWidth(), color = Color.White, shape = RoundedCornerShape(18.dp), elevation = 0.dp) {
        Column(Modifier.padding(24.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text(title, color = Pine, fontSize = 19.sp, fontWeight = FontWeight.SemiBold)
            content()
        }
    }
}
