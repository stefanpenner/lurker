package tui

func helpLineNormal() string {
	return "j/k navigate  ⏎/l expand  ␣ start/pause  f focus  r add repo  R remove  g lazygit  c claude  o open  i info  a approve  q quit"
}

func helpLineLogs() string {
	return "j/k scroll  g/G top/bottom  esc back  o open issue  a approve PR  q quit"
}

func helpLineFocus() string {
	return "j/k scroll  G bottom  ␣ start/pause  o open  a approve  c claude  esc back"
}

func helpLineDialog() string {
	return "esc close"
}
