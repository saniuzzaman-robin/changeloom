package dev.changeloom.shared.data

/** The topic hierarchy from `/v1/topics`. Topics whose parent is unknown are treated as roots. */
class TopicTree(topics: List<Topic>) {
    private val bySlug: Map<String, Topic> = topics.associateBy { it.slug }
    private val children: Map<String, List<Topic>> =
        topics.filter { it.parent != null && it.parent in bySlug }.groupBy { it.parent!! }

    val roots: List<Topic> = topics.filter { it.parent == null || it.parent !in bySlug }
    val slugs: Set<String> get() = bySlug.keys

    operator fun contains(slug: String): Boolean = slug in bySlug
    fun topic(slug: String): Topic? = bySlug[slug]
    fun childrenOf(slug: String): List<Topic> = children[slug].orEmpty()
    fun parentOf(slug: String): String? = bySlug[slug]?.parent?.takeIf { it in bySlug }

    /** [slug] and everything below it. */
    fun subtree(slug: String): List<String> = listOf(slug) + childrenOf(slug).flatMap { subtree(it.slug) }

    fun leavesUnder(slug: String): List<String> = subtree(slug).filter { childrenOf(it).isEmpty() }
}

enum class CheckState { Checked, Unchecked, Partial }

/**
 * Immutable tri-state selection over a [TopicTree]. A topic is checked exactly when it is a selected
 * leaf or all of its children are checked. The backend includes a followed topic's descendants, so
 * [toFollowed] sends only the topmost checked topics.
 */
class TopicSelection private constructor(val tree: TopicTree, private val checked: Set<String>) {

    fun stateOf(slug: String): CheckState = when {
        slug in checked -> CheckState.Checked
        tree.subtree(slug).any { it in checked } -> CheckState.Partial
        else -> CheckState.Unchecked
    }

    /** Checks or unchecks [slug] with its whole subtree, then recomputes its ancestors. */
    fun toggle(slug: String): TopicSelection {
        if (slug !in tree) return this
        val next = checked.toMutableSet()
        if (slug in checked) next -= tree.subtree(slug) else next += tree.subtree(slug)
        fixAncestors(tree, next, slug)
        return TopicSelection(tree, next)
    }

    fun selectAll(): TopicSelection = TopicSelection(tree, tree.slugs)
    fun clear(): TopicSelection = TopicSelection(tree, emptySet())

    /** The minimal set of slugs to follow: checked topics whose parent is not checked. */
    fun toFollowed(): List<String> = checked.filter { tree.parentOf(it) !in checked }.sorted()

    /** Number of checked leaf topics, i.e. how many concrete topics the user will see. */
    val count: Int get() = checked.count { tree.childrenOf(it).isEmpty() }
    val isEmpty: Boolean get() = checked.isEmpty()

    override fun equals(other: Any?): Boolean = other is TopicSelection && other.tree === tree && other.checked == checked
    override fun hashCode(): Int = checked.hashCode()

    companion object {
        fun empty(tree: TopicTree): TopicSelection = TopicSelection(tree, emptySet())

        /** Expands each followed topic to its subtree; unknown slugs are dropped. */
        fun fromFollowed(tree: TopicTree, followed: Collection<String>): TopicSelection {
            val known = followed.filter { it in tree }
            val next = known.flatMapTo(mutableSetOf()) { tree.subtree(it) }
            known.forEach { fixAncestors(tree, next, it) }
            return TopicSelection(tree, next)
        }

        private fun fixAncestors(tree: TopicTree, checked: MutableSet<String>, slug: String) {
            var parent = tree.parentOf(slug)
            while (parent != null) {
                if (tree.childrenOf(parent).all { it.slug in checked }) checked += parent else checked -= parent
                parent = tree.parentOf(parent)
            }
        }
    }
}
