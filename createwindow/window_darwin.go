// +build darwin

package createwindow

import (
	"fmt"

	"../command"
)

// PopulateCommand will take structured data after config file is implemented
func ChromeCommand(layout Layout, userdatadir, display string,
	debuggingPort int) command.ExternalCommand {
	cmd := command.ExternalCommand{
		Path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		Arg: []string{"--user-data-dir=" + userdatadir,
			fmt.Sprintf("--window-position=%d,%d", layout.x, layout.y),
			fmt.Sprintf("--window-size=%d,%d", layout.w, layout.h),
			"--disable-session-crashed-bubble", "--disble-infobars", "--disable-extensions",
			fmt.Sprintf("--remote-debugging-port=%d", debuggingport),
		},
	}
	return cmd
}
