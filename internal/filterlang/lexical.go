package filterlang

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// isSpace reports whether r is whitespace to JavaScript's `\s`, which splits the
// web's filter text. It differs from unicode.IsSpace on U+0085 and U+FEFF.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', '\u2028', '\u2029', '\ufeff':
		return true
	}
	return unicode.Is(unicode.Zs, r)
}

// lower lowercases s with the rules of JavaScript's toLowerCase. It differs
// from strings.ToLower on U+0130, which lowercases to two code points, and on
// a capital sigma that ends a word, which becomes the final sigma U+03C2. The
// cases in internal/filterlangcontract pin both against the web. Character
// properties come from Go's unicode tables, so a letter newer than those
// tables can lowercase differently than in a browser with newer ones.
func lower(s string) string {
	if !strings.ContainsAny(s, "\u0130\u03a3") {
		return strings.ToLower(s)
	}
	var b strings.Builder
	for i, r := range s {
		switch r {
		case '\u0130':
			b.WriteString("i\u0307")
		case '\u03a3':
			if isFinalSigma(s, i) {
				b.WriteRune('\u03c2')
			} else {
				b.WriteRune('\u03c3')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// isFinalSigma applies Unicode's Final_Sigma condition to the capital sigma at
// byte offset i of s: a cased letter precedes it and none follows it, skipping
// case-ignorable characters on both sides.
func isFinalSigma(s string, i int) bool {
	return casedLetterBefore(s[:i]) && !casedLetterAfter(s[i+len("\u03a3"):])
}

func casedLetterBefore(s string) bool {
	for s != "" {
		r, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
		if !isCaseIgnorable(r) {
			return isCased(r)
		}
	}
	return false
}

func casedLetterAfter(s string) bool {
	for _, r := range s {
		if !isCaseIgnorable(r) {
			return isCased(r)
		}
	}
	return false
}

// isCased is Unicode's Cased property, from Go's unicode tables.
func isCased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// isCaseIgnorable is Unicode's Case_Ignorable property: the general categories
// Mn, Me, Cf, Lm and Sk from Go's unicode tables, and the word-break classes
// MidLetter, MidNumLet and Single_Quote, which the switch lists.
func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '\u00b7', '\u0387', '\u055f', '\u05f4', '\u2018', '\u2019', '\u2024', '\u2027',
		'\ufe13', '\ufe52', '\ufe55', '\uff07', '\uff0e', '\uff1a':
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}
