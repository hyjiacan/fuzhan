package resources

import _ "embed"

//go:embed usage.txt
var Usage string

//go:embed cli_help.txt
var CliHelpTpl string

//go:embed cli_search_error.txt
var CliSearchErrorTpl string

//go:embed launchd.plist.txt
var LaunchdPlistTemplate string

//go:embed systemd.service.txt
var SystemdServiceTemplate string

//go:embed lite_page.html
var LitePageTpl string
