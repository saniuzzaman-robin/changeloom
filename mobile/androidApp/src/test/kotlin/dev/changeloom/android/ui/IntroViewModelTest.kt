package dev.changeloom.android.ui

import org.junit.Rule
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class IntroViewModelTest {
    @get:Rule val main = MainDispatcherRule()

    @Test
    fun showsOnAFreshInstall() {
        val vm = IntroViewModel(MemoryIntroStore(), main.dispatcher)
        assertEquals(true, vm.show.value)
    }

    @Test
    fun hiddenOnceSeen() {
        val vm = IntroViewModel(MemoryIntroStore(introSeen = true), main.dispatcher)
        assertEquals(false, vm.show.value)
    }

    @Test
    fun finishHidesAndRemembers() {
        val store = MemoryIntroStore()
        val vm = IntroViewModel(store, main.dispatcher)
        vm.finish()
        assertEquals(false, vm.show.value)
        assertTrue(store.introSeen)
        assertFalse(IntroViewModel(store, main.dispatcher).show.value!!)
    }
}
