package tui

// DefaultRegistry maps every command name and alias to a view constructor.
// Aliases are listed explicitly rather than derived, so the set is greppable
// and a typo cannot silently shadow another command.
func DefaultRegistry() map[string]func() View {
	projects := func() View { return ProjectsView{} }
	models := func() View { return ModelsView{} }
	sessions := func() View { return SessionsView{} }
	requests := func() View { return RequestsView{} }
	agents := func() View { return AgentsView{} }
	workflows := func() View { return WorkflowsView{} }
	limits := func() View { return LimitsView{} }
	days := func() View { return DaysView{} }
	unpriced := func() View { return UnpricedView{} }
	pulse := func() View { return PulseView{} }
	return map[string]func() View{
		"projects": projects, "proj": projects, "p": projects,
		"models": models, "model": models, "m": models,
		"sessions": sessions, "sess": sessions, "s": sessions,
		"requests": requests, "req": requests, "r": requests,
		"agents": agents, "agent": agents, "a": agents,
		"workflows": workflows, "wf": workflows, "w": workflows,
		"limits": limits, "limit": limits, "l": limits,
		"days": days, "day": days, "d": days,
		"unpriced": unpriced, "unp": unpriced, "u": unpriced,
		"pulse": pulse, "pu": pulse,
	}
}
