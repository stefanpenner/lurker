package tui

func helpLineNormal() string {
	return "j/k navigate  l logs  g lazygit  c claude  o open issue  i info  p pause  a approve PR  q quit"
}

func helpLineLogs() string {
	return "j/k scroll  g/G top/bottom  esc back  o open issue  a approve PR  q quit"
}

func helpLineDialog() string {
	return "esc close"
}
