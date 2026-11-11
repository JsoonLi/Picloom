// Package locale says the studio's texts in the language the user chose.
//
// Texts are written in Simplified Chinese, which is also the key of their
// translations: T looks a text up in the catalog of the current language
// and falls back to the Chinese source when it has none.
package locale

import "sync/atomic"

const (
	// Chinese is the source language of every text.
	Chinese = "zh-Hans"
	English = "en"
)

// Supported are the languages the studio is translated to, source first.
var Supported = []string{Chinese, English}

var current atomic.Pointer[string]

// Set changes the language texts are said in. Tags other than English's
// say the source language. Safe from any goroutine.
func Set(tag string) {
	tag = Normalize(tag)
	current.Store(&tag)
}

// Current returns the language texts are said in.
func Current() string {
	if tag := current.Load(); tag != nil {
		return *tag
	}
	return Chinese
}

// Normalize maps a language tag to one the studio supports: English for
// any English tag, else Chinese.
func Normalize(tag string) string {
	if tag == English || len(tag) > 2 && tag[:2] == English && (tag[2] == '-' || tag[2] == '_') {
		return English
	}
	return Chinese
}

// T returns text in the current language.
func T(text string) string {
	if Current() == English {
		if translated, ok := english[text]; ok {
			return translated
		}
	}
	return text
}

// Error is an error whose message follows the current language each time
// it is read, for errors made once and compared with errors.Is.
type Error string

func (e Error) Error() string { return T(string(e)) }
