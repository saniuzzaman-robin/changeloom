package dev.changeloom.shared

import dev.changeloom.shared.data.CheckState
import dev.changeloom.shared.data.Topic
import dev.changeloom.shared.data.TopicSelection
import dev.changeloom.shared.data.TopicTree
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

private fun topic(slug: String, parent: String? = null) = Topic(slug, slug, parent, "")

private val tree = TopicTree(
    listOf(
        topic("lang"),
        topic("lang/go", "lang"),
        topic("lang/rust", "lang"),
        topic("web"),
        topic("web/react", "web"),
        topic("web/js", "web"),
        topic("web/js/node", "web/js"),
        topic("web/js/deno", "web/js"),
        topic("misc"),
    ),
)

class TopicSelectionTest {
    @Test
    fun toggleRootCascadesDown() {
        val s = TopicSelection.empty(tree).toggle("web")
        assertEquals(CheckState.Checked, s.stateOf("web/js/node"))
        assertEquals(CheckState.Checked, s.stateOf("web/react"))
        assertEquals(listOf("web"), s.toFollowed())
        assertEquals(3, s.count)

        val off = s.toggle("web")
        assertTrue(off.isEmpty)
        assertEquals(CheckState.Unchecked, off.stateOf("web/js"))
    }

    @Test
    fun someChildrenMakeAncestorsPartial() {
        val s = TopicSelection.empty(tree).toggle("web/js/node")
        assertEquals(CheckState.Partial, s.stateOf("web/js"))
        assertEquals(CheckState.Partial, s.stateOf("web"))
        assertEquals(CheckState.Unchecked, s.stateOf("lang"))
        assertEquals(listOf("web/js/node"), s.toFollowed())
    }

    @Test
    fun allChildrenCollapseToRoot() {
        val s = TopicSelection.empty(tree).toggle("web/react").toggle("web/js/node").toggle("web/js/deno")
        assertEquals(CheckState.Checked, s.stateOf("web/js"))
        assertEquals(CheckState.Checked, s.stateOf("web"))
        assertEquals(listOf("web"), s.toFollowed())
    }

    @Test
    fun uncheckingChildUnchecksAncestors() {
        val s = TopicSelection.empty(tree).toggle("web").toggle("web/js/deno")
        assertEquals(CheckState.Partial, s.stateOf("web"))
        assertEquals(CheckState.Partial, s.stateOf("web/js"))
        assertEquals(listOf("web/js/node", "web/react"), s.toFollowed())
    }

    @Test
    fun fromFollowedExpandsDescendantsAndFixesAncestors() {
        val s = TopicSelection.fromFollowed(tree, listOf("lang", "web/js"))
        assertEquals(CheckState.Checked, s.stateOf("lang/rust"))
        assertEquals(CheckState.Checked, s.stateOf("web/js/deno"))
        assertEquals(CheckState.Partial, s.stateOf("web"))
        assertEquals(listOf("lang", "web/js"), s.toFollowed())

        val collapsed = TopicSelection.fromFollowed(tree, listOf("lang/go", "lang/rust"))
        assertEquals(listOf("lang"), collapsed.toFollowed())
    }

    @Test
    fun leafRootsToggleOnTheirOwn() {
        val s = TopicSelection.empty(tree).toggle("misc")
        assertEquals(CheckState.Checked, s.stateOf("misc"))
        assertEquals(listOf("misc"), s.toFollowed())
        assertEquals(1, s.count)
    }

    @Test
    fun unknownSlugsAreIgnored() {
        val s = TopicSelection.fromFollowed(tree, listOf("gone", "misc"))
        assertEquals(listOf("misc"), s.toFollowed())
        assertEquals(s, s.toggle("gone"))
    }

    @Test
    fun orphanTopicsBecomeRoots() {
        val t = TopicTree(listOf(topic("a/b", "a"), topic("c")))
        assertEquals(listOf("a/b", "c"), t.roots.map { it.slug })
    }

    @Test
    fun selectAllAndClear() {
        val all = TopicSelection.empty(tree).selectAll()
        assertEquals(listOf("lang", "misc", "web"), all.toFollowed())
        assertEquals(6, all.count)
        assertTrue(all.clear().isEmpty)
    }
}
