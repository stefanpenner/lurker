package tui

// fmtHelp renders a single key hint in LazyVim style: "<key> action"
func fmtHelp(key, action string) string {
	return footerKeyStyle.Render(key) + " " + footerStyle.Render(action)
}

func helpLineNormal() string {
	sep := footerSepStyle.Render("  |  ")
	return fmtHelp("j/k", "navigate") + sep +
		fmtHelp("enter/l", "expand") + sep +
		fmtHelp("space", "start/pause") + sep +
		fmtHelp("f", "focus") + sep +
		fmtHelp("r", "add repo") + sep +
		fmtHelp("R", "remove") + sep +
		fmtHelp("g", "lazygit") + sep +
		fmtHelp("c", "claude") + sep +
		fmtHelp("o", "open") + sep +
		fmtHelp("i", "info") + sep +
		fmtHelp("a", "approve") + sep +
		fmtHelp("q", "quit")
}

func helpLineLogs() string {
	sep := footerSepStyle.Render("  |  ")
	return fmtHelp("j/k", "scroll") + sep +
		fmtHelp("g/G", "top/bottom") + sep +
		fmtHelp("esc", "back") + sep +
		fmtHelp("o", "open issue") + sep +
		fmtHelp("a", "approve PR") + sep +
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
