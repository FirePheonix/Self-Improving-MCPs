package improvementsandbox

// ResolveRoute applies the overlay's routing rules to a requested tool name,
// exactly as the proxy does at call time. It returns the tool that will actually
// be served, whether the call is denied, and the rule's note.
//
// The first rule matching FromTool that either denies or names a ToTool wins;
// rules matching neither are skipped. Keep this the single source of truth: the
// proxy and the selection gate both call it so the gate judges what production does.
func (o Overlay) ResolveRoute(tool string) (routed string, denied bool, note string) {
	for _, r := range o.Routing {
		if r.FromTool != tool {
			continue
		}
		if r.Deny {
			return tool, true, r.Note
		}
		if r.ToTool != "" {
			return r.ToTool, false, r.Note
		}
	}
	return tool, false, ""
}
