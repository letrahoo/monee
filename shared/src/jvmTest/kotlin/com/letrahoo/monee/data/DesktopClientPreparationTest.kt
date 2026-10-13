package com.letrahoo.monee.data

import java.awt.EventQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.swing.Swing
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class DesktopClientPreparationTest {
    @Test fun blockedConfigurationReadDoesNotBlockEdtOrFinishPreparation() = runBlocking {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val eventHandled = CountDownLatch(1)
        val readOnEdt = AtomicBoolean(true)
        val prepared = AtomicBoolean(false)
        val job = async(Dispatchers.Swing) {
            prepareDesktopClientConfiguration {
                readOnEdt.set(EventQueue.isDispatchThread())
                started.countDown()
                check(release.await(10, TimeUnit.SECONDS))
                Result.success(null)
            }
            prepared.set(true)
        }
        try {
            assertTrue(started.await(5, TimeUnit.SECONDS))
            EventQueue.invokeLater { eventHandled.countDown() }
            assertTrue(eventHandled.await(5, TimeUnit.SECONDS), "EDT must remain responsive during file access")
            assertFalse(readOnEdt.get())
            assertFalse(prepared.get(), "Client creation must wait for configuration")
        } finally {
            release.countDown()
        }
        job.await()
        assertTrue(prepared.get())
    }

    @Test fun cancelledPreparationCannotEnterClientCreation() = runBlocking {
        val started = CountDownLatch(1)
        val release = CountDownLatch(1)
        val prepared = AtomicBoolean(false)
        val job = async(Dispatchers.Swing) {
            prepareDesktopClientConfiguration {
                started.countDown()
                check(release.await(10, TimeUnit.SECONDS))
                Result.success(null)
            }
            prepared.set(true)
        }
        try {
            assertTrue(started.await(5, TimeUnit.SECONDS))
            job.cancel()
        } finally {
            release.countDown()
        }
        job.join()
        assertFalse(prepared.get())
    }
}
