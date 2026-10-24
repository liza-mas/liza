package oracle

import (
	"fmt"
	"strings"
)

// windowsInvalidChars are the printable characters Windows rejects in a path
// component.
const windowsInvalidChars = `<>:"|?*\`

// WindowsPathProblem reports why a slash-separated path cannot be checked out
// on Windows, or "" when every component is valid there. A component is
// invalid when it holds a character of <>:"|?*\, a byte below 0x20, a trailing
// dot or space, or a reserved device name (CON, PRN, AUX, NUL, COM1-COM9,
// LPT1-LPT9, any case, with or without extension). The reason names the
// component, never the rest of the path.
func WindowsPathProblem(p string) string {
	for component := range strings.SplitSeq(p, "/") {
		if reason := windowsComponentProblem(component); reason != "" {
			return fmt.Sprintf("component %q %s", component, reason)
		}
	}
	return ""
}

func windowsComponentProblem(component string) string {
	for i := 0; i < len(component); i++ {
		c := component[i]
		switch {
		case c < 0x20:
			return fmt.Sprintf("holds control byte 0x%02X", c)
		case strings.IndexByte(windowsInvalidChars, c) >= 0:
			return fmt.Sprintf("holds %q, which Windows rejects", string(c))
		}
	}
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return "ends with a dot or space"
	}
	stem, _, _ := strings.Cut(component, ".")
	if isWindowsDeviceName(stem) {
		return "is a reserved Windows device name"
	}
	return ""
}

func isWindowsDeviceName(stem string) bool {
	switch s := strings.ToUpper(stem); s {
	case "CON", "PRN", "AUX", "NUL":
		return true
	default:
		return len(s) == 4 && (strings.HasPrefix(s, "COM") || strings.HasPrefix(s, "LPT")) && s[3] >= '1' && s[3] <= '9'
	}
}
