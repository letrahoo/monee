package com.letrahoo.monee.data
import kotlinx.coroutines.runBlocking
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertTrue
class SessionVaultTest {
 @Test fun keychainRoundTripWhenRequested() = runBlocking {
  if(System.getenv("MONEE_TEST_KEYCHAIN")!="1")return@runBlocking
  val origin="http://127.0.0.1:1/session-test-"+java.util.UUID.randomUUID()
  val token="qa_"+"x".repeat(40)
  try {
   SessionVault.save(origin,token)
   assertEquals(token,SessionVault.load(origin))
   SessionVault.clear(origin)
   assertNull(SessionVault.load(origin))
  } finally {SessionVault.clear(origin)}
 }
 @Test fun credentialsCannotInjectKeychainCommands(){
  assertTrue(SessionVault.valid("a".repeat(43)))
  listOf("", "a\nquit", "a b", "\";delete-keychain", "a".repeat(257)).forEach {assertFalse(SessionVault.valid(it))}
 }
 @Test fun separateDataDirectoriesAndOriginsNeverShareCredentials(){
  val main=SessionVault.account("http://127.0.0.1:4173","/test/main")
  assertNotEquals(main,SessionVault.account("http://127.0.0.1:4173","/test/qa"))
  assertNotEquals(main,SessionVault.account("http://127.0.0.1:4273","/test/main"))
 }
}
