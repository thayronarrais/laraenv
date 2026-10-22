package sourcecontrol

import "regexp"

var urlCredentials = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@]+@`)

// Bounded output can end halfway through userinfo, before its @. Hide a
// trailing colon-bearing authority conservatively rather than exposing it.
var incompleteCredentials = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@:]+:[^\s/@]*$`)

func redact(text string) string {
	text = urlCredentials.ReplaceAllString(text, "${1}[redacted]@")
	return incompleteCredentials.ReplaceAllString(text, "${1}[redacted]")
}
