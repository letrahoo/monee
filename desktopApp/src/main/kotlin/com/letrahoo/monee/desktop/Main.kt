package com.letrahoo.monee.desktop

import androidx.compose.ui.unit.dp
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.window.Window
import androidx.compose.ui.window.application
import androidx.compose.ui.window.rememberWindowState
import com.letrahoo.monee.App
import com.letrahoo.monee.data.DesktopReturn

fun main() {
    DesktopReturn.install()
    application {
        Window(
            onCloseRequest = ::exitApplication,
            title = "Monee · Know Your Money. Own Your Future.",
            state = rememberWindowState(width = 1180.dp, height = 850.dp),
        ) {
            DisposableEffect(window) {
                DesktopReturn.attach(window)
                onDispose { DesktopReturn.detach(window) }
            }
            App()
        }
    }
}
