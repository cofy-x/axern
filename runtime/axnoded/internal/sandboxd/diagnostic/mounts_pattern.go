package diagnostic

import "regexp"

var mountInfoOctalEscapePattern = regexp.MustCompile(`\\[0-7]{3}`)
