package info.warplabs.warpdrop

/** What only the host OS can do. Results come back through [AppController]. */
interface Platform {
    fun pickFiles()
    fun scanCode()
    fun copy(text: String)
    fun share(text: String)
    fun openDownloads()
}
