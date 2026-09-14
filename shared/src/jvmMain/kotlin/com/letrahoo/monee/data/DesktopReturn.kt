package com.letrahoo.monee.data

import java.awt.Desktop
import java.awt.EventQueue
import java.awt.Frame
import java.net.URI

/** Window activation only. Deep links never supply credentials or change login state. */
object DesktopReturn {
    private var window: Frame? = null
    private var pending = false

    fun install() {
        val desktop = desktopOrNull() ?: return
        if (desktop.isSupported(Desktop.Action.APP_OPEN_URI)) {
            try {
                desktop.setOpenURIHandler { event ->
                    if (accepts(event.uri)) activate()
                }
            } catch (_: SecurityException) {
            } catch (_: UnsupportedOperationException) { }
        }
    }

    fun attach(frame: Frame) = onEventThread {
        window = frame
        if (pending) activate()
    }

    fun detach(frame: Frame) = onEventThread {
        if (window === frame) window = null
    }

    fun activate() = onEventThread {
        val frame = window
        if (frame == null) {
            pending = true
        } else {
            pending = false
            frame.extendedState = frame.extendedState and Frame.ICONIFIED.inv()
            frame.isVisible = true
            val desktop = desktopOrNull()
            if (desktop?.isSupported(Desktop.Action.APP_REQUEST_FOREGROUND) == true) {
                // Focus is best effort: an OS restriction must not invalidate a completed login.
                try { desktop.requestForeground(true) } catch (_: SecurityException) {
                } catch (_: UnsupportedOperationException) { }
            }
            frame.toFront()
            frame.requestFocus()
        }
    }

    internal fun accepts(uri: URI): Boolean =
        uri.scheme == "monee" && uri.rawAuthority == "auth" && uri.rawPath == "/complete" &&
            uri.rawQuery == null && uri.rawFragment == null

    private fun desktopOrNull(): Desktop? = try {
        if (Desktop.isDesktopSupported()) Desktop.getDesktop() else null
    } catch (_: SecurityException) { null }

    private fun onEventThread(action: () -> Unit) {
        if (EventQueue.isDispatchThread()) action() else EventQueue.invokeLater(action)
    }
}
