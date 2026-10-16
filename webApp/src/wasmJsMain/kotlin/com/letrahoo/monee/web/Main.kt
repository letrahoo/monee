@file:OptIn(kotlin.js.ExperimentalWasmJsInterop::class)

package com.letrahoo.monee.web

import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.window.ComposeViewport
import com.letrahoo.monee.App

@JsFun("() => { if (typeof window.moneeStartupReady === 'function') window.moneeStartupReady(); }")
private external fun markStartupReady()

@OptIn(ExperimentalComposeUiApi::class)
fun main() {
    ComposeViewport(viewportContainerId = "monee") {
        App()
        LaunchedEffect(Unit) {
            withFrameNanos { }
            markStartupReady()
        }
    }
}
