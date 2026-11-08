package dev.changeloom.android.ui

import kotlinx.coroutines.flow.MutableStateFlow
import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlin.time.Duration.Companion.minutes
import kotlin.time.TestTimeSource

class ReloadPolicyTest {
    private val time = TestTimeSource()
    private val changes = MutableStateFlow(0)
    private val policy = ReloadPolicy(time, changes, maxAge = 10.minutes)

    @Test
    fun dueUntilTheFirstLoadThenUntilItIsOld() {
        assertTrue(policy.due)
        policy.started()
        assertFalse(policy.due)
        time += 9.minutes
        assertFalse(policy.due)
        time += 1.minutes
        assertTrue(policy.due)
    }

    @Test
    fun aChangeMakesItDueEvenWhileTheLoadRuns() {
        policy.started()
        changes.value++
        assertTrue(policy.due)
        policy.started()
        assertFalse(policy.due)
    }

    @Test
    fun aFailedLoadIsDueAgain() {
        policy.started()
        policy.failed()
        assertTrue(policy.due)
    }
}
