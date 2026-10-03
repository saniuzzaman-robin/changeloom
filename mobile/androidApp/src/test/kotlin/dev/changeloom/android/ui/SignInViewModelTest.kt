package dev.changeloom.android.ui

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.test.runTest
import org.junit.Rule
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse

class SignInViewModelTest {
    @get:Rule val main = MainDispatcherRule()

    @Test
    fun formSurvivesInSavedStateButThePasswordDoesNot() = runTest {
        val saved = SavedStateHandle()
        val vm = SignInViewModel(FakeAuth(), NoAnalytics, testStrings, saved)
        vm.setMode(AuthMode.Register)
        vm.setEmail("ada@example.com")
        vm.setPassword("secret")

        val restored = SignInViewModel(FakeAuth(), NoAnalytics, testStrings, SavedStateHandle(saved.keys().associateWith { saved.get<Any>(it) }))
        assertEquals(AuthMode.Register, restored.mode.value)
        assertEquals("ada@example.com", restored.email.value)
        assertEquals("", restored.password.value)
        assertFalse(saved.keys().any { saved.get<Any>(it) == "secret" })
    }

    @Test
    fun submitUsesTheModeAndTrimsTheEmail() = runTest {
        val auth = FakeAuth()
        val vm = SignInViewModel(auth, NoAnalytics, testStrings, SavedStateHandle())
        vm.setEmail("  ada@example.com ")
        vm.setPassword("pw")
        vm.submit()
        vm.setMode(AuthMode.Register)
        vm.submit()
        assertEquals(listOf("signIn:ada@example.com:pw", "register:ada@example.com:pw"), auth.calls)
    }
}
