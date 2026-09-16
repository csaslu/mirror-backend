package meta

import (
	"runtime"
)

const UserAgent = "LidaUniversityMirror/" + Version + " " + Nickname + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
