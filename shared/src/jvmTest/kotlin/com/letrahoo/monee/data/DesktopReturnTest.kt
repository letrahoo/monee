package com.letrahoo.monee.data

import java.net.URI
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class DesktopReturnTest {
    @Test fun onlyTheFixedCredentialFreeReturnLinkIsAccepted() {
        assertTrue(DesktopReturn.accepts(URI("monee://auth/complete")))
        listOf(
            "https://auth/complete", "monee://attacker/complete", "monee://auth/other",
            "monee://user@auth/complete", "monee://auth:80/complete", "monee://auth/complete/",
            "monee://auth/complete?token=untrusted", "monee://auth/complete#untrusted",
            "monee://auth/%63omplete", "monee:auth/complete",
        ).forEach { assertFalse(DesktopReturn.accepts(URI(it)), it) }
    }
}
