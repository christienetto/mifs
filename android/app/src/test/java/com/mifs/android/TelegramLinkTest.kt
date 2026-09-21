package com.mifs.android

import org.junit.Assert.*
import org.junit.Test

class TelegramLinkTest {
    @Test fun serverMifMatchesIosAndMiniAppCode() {
        val code = TelegramLink.startParameter("https://mifs.cgn.fi/m/abcdefghijkl")!!
        assertEquals("s2_abcdefghijkl", code)
        assertEquals("tg://resolve?domain=MIFSAppBot&startapp=s2_abcdefghijkl&mode=compact", TelegramLink.appURL(code))
        assertEquals("https://t.me/MIFSAppBot?startapp=s2_abcdefghijkl&mode=compact", TelegramLink.webURL(code))
    }
    @Test fun rejectsLinksTheBotCannotResolve() {
        listOf("", "https://localhost/m/abcdefghijkl", "http://mifs.cgn.fi/m/abcdefghijkl",
            "https://mifs.cgn.fi:8080/m/abcdefghijkl", "https://mifs.cgn.fi/m/nope",
            "https://mifs.cgn.fi/m/abcdefghijkl/audio", "https://mifs.cgn.fi/other/abcdefghijkl").forEach {
            assertNull(it, TelegramLink.startParameter(it))
        }
    }
}
