package main

// Culture is a language clients offer in their language pickers.
type Culture struct {
	Name                        string   `json:"Name"`
	DisplayName                 string   `json:"DisplayName"`
	TwoLetterISOLanguageName    string   `json:"TwoLetterISOLanguageName"`
	ThreeLetterISOLanguageName  string   `json:"ThreeLetterISOLanguageName"`
	ThreeLetterISOLanguageNames []string `json:"ThreeLetterISOLanguageNames"`
}

// culture takes ISO 639-1, then every ISO 639-2 code, bibliographic first.
func culture(twoLetter, name string, threeLetter ...string) Culture {
	return Culture{
		Name:                        name,
		DisplayName:                 name,
		TwoLetterISOLanguageName:    twoLetter,
		ThreeLetterISOLanguageName:  threeLetter[0],
		ThreeLetterISOLanguageNames: threeLetter,
	}
}

var cultures = []Culture{
	culture("ar", "Arabic", "ara"),
	culture("be", "Belarusian", "bel"),
	culture("bg", "Bulgarian", "bul"),
	culture("cs", "Czech", "cze", "ces"),
	culture("da", "Danish", "dan"),
	culture("de", "German", "ger", "deu"),
	culture("el", "Greek", "gre", "ell"),
	culture("en", "English", "eng"),
	culture("es", "Spanish", "spa"),
	culture("et", "Estonian", "est"),
	culture("fa", "Persian", "per", "fas"),
	culture("fi", "Finnish", "fin"),
	culture("fr", "French", "fre", "fra"),
	culture("he", "Hebrew", "heb"),
	culture("hi", "Hindi", "hin"),
	culture("hr", "Croatian", "hrv"),
	culture("hu", "Hungarian", "hun"),
	culture("id", "Indonesian", "ind"),
	culture("it", "Italian", "ita"),
	culture("ja", "Japanese", "jpn"),
	culture("kk", "Kazakh", "kaz"),
	culture("ko", "Korean", "kor"),
	culture("lt", "Lithuanian", "lit"),
	culture("lv", "Latvian", "lav"),
	culture("nl", "Dutch", "dut", "nld"),
	culture("no", "Norwegian", "nor"),
	culture("pl", "Polish", "pol"),
	culture("pt", "Portuguese", "por"),
	culture("ro", "Romanian", "rum", "ron"),
	culture("ru", "Russian", "rus"),
	culture("sk", "Slovak", "slo", "slk"),
	culture("sr", "Serbian", "srp"),
	culture("sv", "Swedish", "swe"),
	culture("th", "Thai", "tha"),
	culture("tr", "Turkish", "tur"),
	culture("uk", "Ukrainian", "ukr"),
	culture("vi", "Vietnamese", "vie"),
	culture("zh", "Chinese", "chi", "zho"),
}
