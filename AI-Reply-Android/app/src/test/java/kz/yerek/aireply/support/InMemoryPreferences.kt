package kz.yerek.aireply.support

import android.content.SharedPreferences

/**
 * SharedPreferences without Android: a map, applied immediately, and safe to
 * use from several threads like the real one.
 */
class InMemoryPreferences : SharedPreferences {
    private val values = HashMap<String, Any?>()

    private fun <T> read(block: (Map<String, Any?>) -> T): T = synchronized(values) { block(values) }

    override fun getAll(): MutableMap<String, *> = read { HashMap(it) }
    override fun getString(key: String?, defValue: String?): String? = read { it[key] as String? ?: defValue }

    @Suppress("UNCHECKED_CAST")
    override fun getStringSet(key: String?, defValues: MutableSet<String>?): MutableSet<String>? =
        read { (it[key] as Set<String>?)?.toMutableSet() ?: defValues }

    override fun getInt(key: String?, defValue: Int): Int = read { it[key] as Int? ?: defValue }
    override fun getLong(key: String?, defValue: Long): Long = read { it[key] as Long? ?: defValue }
    override fun getFloat(key: String?, defValue: Float): Float = read { it[key] as Float? ?: defValue }
    override fun getBoolean(key: String?, defValue: Boolean): Boolean = read { it[key] as Boolean? ?: defValue }
    override fun contains(key: String?): Boolean = read { it.containsKey(key) }
    override fun edit(): SharedPreferences.Editor = Editor()
    override fun registerOnSharedPreferenceChangeListener(
        listener: SharedPreferences.OnSharedPreferenceChangeListener?
    ) = Unit
    override fun unregisterOnSharedPreferenceChangeListener(
        listener: SharedPreferences.OnSharedPreferenceChangeListener?
    ) = Unit

    private inner class Editor : SharedPreferences.Editor {
        private val pending = HashMap<String, Any?>()
        private val removed = HashSet<String>()
        private var clearAll = false

        private fun put(key: String?, value: Any?): SharedPreferences.Editor {
            pending[requireNotNull(key)] = value
            return this
        }

        override fun putString(key: String?, value: String?) = put(key, value)
        override fun putStringSet(key: String?, values: MutableSet<String>?) = put(key, values?.toSet())
        override fun putInt(key: String?, value: Int) = put(key, value)
        override fun putLong(key: String?, value: Long) = put(key, value)
        override fun putFloat(key: String?, value: Float) = put(key, value)
        override fun putBoolean(key: String?, value: Boolean) = put(key, value)

        override fun remove(key: String?): SharedPreferences.Editor {
            removed += requireNotNull(key)
            return this
        }

        override fun clear(): SharedPreferences.Editor {
            clearAll = true
            return this
        }

        override fun commit(): Boolean {
            apply()
            return true
        }

        override fun apply() {
            synchronized(values) {
                if (clearAll) values.clear()
                removed.forEach(values::remove)
                values.putAll(pending)
            }
        }
    }
}
