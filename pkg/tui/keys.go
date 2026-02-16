package tui

// fmtHelp renders a single key hint in LazyVim style: "<key> action"
func fmtHelp(key, action string) string {
	return footerKeyStyle.Render(key) + " " + footerStyle.Render(action)
}

func helpLineNormal() string {
	sep := footerSepStyle.Render("  |  ")
	return fmtHelp("j/k", "navigate") + sep +
		fmtHelp("enter", "focus") + sep +
		fmtHelp("space", "start/pause") + sep +
		fmtHelp("r", "add repo") + sep +
		fmtHelp("a", "approve") + sep +
		fmtHelp("?", "help") + sep +
		fmtHelp("q", "quit")
}

func helpLineFocus() string {
	sep := footerSepStyle.Render("  |  ")
	return fmtHelp("j/k", "scroll") + sep +
		fmtHelp("G", "bottom") + sep +
		fmtHelp("space", "start/pause") + sep +
		fmtHelp("o", "open") + sep +
		fmtHelp("a", "approve") + sep +
		fmtHelp("c", "claude") + sep +
		fmtHelp("esc", "back")
}

func helpLineDialog() string {
	return fmtHelp("esc", "close")
}
