package fal

import "strings"

const inlineDataScheme = "data:"

// capturableLink drops the media fal returns inline in sync mode: a data URI is the whole file,
// megabytes per image, and the record would carry it to Revenium and hold it in the retry buffer.
func capturableLink(url string) string {
	if len(url) >= len(inlineDataScheme) && strings.EqualFold(url[:len(inlineDataScheme)], inlineDataScheme) {
		return ""
	}
	return url
}
